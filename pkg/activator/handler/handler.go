/*
Copyright 2018 The Knative Authors
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
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/reconciler"
	"go.uber.org/zap"
)

// ActivationHandler will wait for an active endpoint for a revision
// to be available before proxing the request
type ActivationHandler struct {
	Activator activator.Activator
	Logger    *zap.SugaredLogger
	Transport http.RoundTripper
}

func (a *ActivationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	namespace := r.Header.Get(reconciler.GetRevisionHeaderNamespace())
	name := r.Header.Get(reconciler.GetRevisionHeaderName())
	config := r.Header.Get(reconciler.GetConfigurationHeader())

	endpoint, status, err := a.Activator.ActiveEndpoint(namespace, config, name)

	if err != nil {
		msg := fmt.Sprintf("Error getting active endpoint: %v", err)

		a.Logger.Errorf(msg)
		http.Error(w, msg, int(status))
		return
	}

	target := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", endpoint.FQDN, endpoint.Port),
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = a.Transport

	proxy.ServeHTTP(w, r)
}
