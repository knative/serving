/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"go.uber.org/zap"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	network "knative.dev/pkg/network"
	"knative.dev/serving/pkg/activator"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1"
)

// NewContextHandler creates a handler that extracts the necessary context from the request
// and makes it available on the request's context.
func NewContextHandler(ctx context.Context, next http.Handler) http.Handler {
	return &contextHandler{
		nextHandler:    next,
		revisionLister: revisioninformer.Get(ctx).Lister(),
		logger:         logging.FromContext(ctx),
	}
}

// contextHandler enriches the request's context with structured data.
type contextHandler struct {
	revisionLister servinglisters.RevisionLister
	logger         *zap.SugaredLogger
	nextHandler    http.Handler
}

func (h *contextHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespace := r.Header.Get(activator.RevisionHeaderNamespace)
	name := r.Header.Get(activator.RevisionHeaderName)

	// If the headers aren't explicitly specified, then decode the revision
	// name and namespace from the Host header.
	if name == "" || namespace == "" {
		parts := strings.SplitN(r.Host, ".", 4)
		if len(parts) == 4 && parts[2] == "svc" && parts[3] == network.GetClusterDomainName() {
			name, namespace = parts[0], parts[1]
		}
	}

	revID := types.NamespacedName{Namespace: namespace, Name: name}
	logger := h.logger.With(zap.String(logkey.Key, revID.String()))

	revision, err := h.revisionLister.Revisions(namespace).Get(name)
	if err != nil {
		logger.Errorw("Error while getting revision", zap.Error(err))
		sendError(err, w)
		return
	}

	ctx := r.Context()
	ctx = logging.WithLogger(ctx, logger)
	ctx = withRevision(ctx, revision)
	ctx = WithRevID(ctx, revID)

	h.nextHandler.ServeHTTP(w, r.WithContext(ctx))
}

func sendError(err error, w http.ResponseWriter) {
	msg := fmt.Sprint("Error getting active endpoint: ", err)
	if k8serrors.IsNotFound(err) {
		http.Error(w, msg, http.StatusNotFound)
		return
	}
	http.Error(w, msg, http.StatusInternalServerError)
}
