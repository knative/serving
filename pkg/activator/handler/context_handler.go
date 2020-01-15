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

	"go.uber.org/zap"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
)

type revisionKey struct{}
type revIDKey struct{}

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
	ctx = context.WithValue(ctx, revisionKey{}, revision)
	ctx = context.WithValue(ctx, revIDKey{}, revID)

	h.nextHandler.ServeHTTP(w, r.WithContext(ctx))
}

func withRevision(ctx context.Context, rev *v1alpha1.Revision) context.Context {
	return context.WithValue(ctx, revisionKey{}, rev)
}

func revisionFrom(ctx context.Context) *v1alpha1.Revision {
	return ctx.Value(revisionKey{}).(*v1alpha1.Revision)
}

func withRevID(ctx context.Context, revID types.NamespacedName) context.Context {
	return context.WithValue(ctx, revIDKey{}, revID)
}

func revIDFrom(ctx context.Context) types.NamespacedName {
	return ctx.Value(revIDKey{}).(types.NamespacedName)
}

func sendError(err error, w http.ResponseWriter) {
	msg := fmt.Sprintf("Error getting active endpoint: %v", err)
	if k8serrors.IsNotFound(err) {
		http.Error(w, msg, http.StatusNotFound)
		return
	}
	http.Error(w, msg, http.StatusInternalServerError)
}
