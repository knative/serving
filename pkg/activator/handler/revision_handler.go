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
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	v1a1inf "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	"k8s.io/client-go/tools/cache"
)

// NewRevisionHandler creates a new RevisionHandler.
func NewRevisionHandler(routeInformer v1a1inf.RouteInformer, next http.Handler) *RevisionHandler {
	rand.Seed(time.Now().Unix())

	handler := &RevisionHandler{
		nextHandler:   next,
		domainMapping: make(map[string]*v1alpha1.Route),
	}

	routeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    handler.addRoute,
		UpdateFunc: controller.PassNew(handler.addRoute),
		DeleteFunc: handler.deleteRoute,
	})

	return handler
}

// RevisionHandler infers a target revision if no target headers are set.
type RevisionHandler struct {
	nextHandler http.Handler

	domainMux     sync.RWMutex
	domainMapping map[string]*v1alpha1.Route
}

func (h *RevisionHandler) addRoute(r interface{}) {
	route := r.(*v1alpha1.Route)

	h.domainMux.Lock()
	defer h.domainMux.Unlock()

	h.domainMapping[route.Status.Domain] = route
}

func (h *RevisionHandler) deleteRoute(r interface{}) {
	route := r.(*v1alpha1.Route)

	h.domainMux.Lock()
	defer h.domainMux.Unlock()

	delete(h.domainMapping, route.Name)
}

func (h *RevisionHandler) getRoute(domain string) *v1alpha1.Route {
	h.domainMux.RLock()
	defer h.domainMux.RUnlock()

	route, _ := h.domainMapping[domain]
	return route
}

func (h *RevisionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only try to infer a revision if the header is not already set.
	if r.Header.Get(activator.RevisionHeaderName) == "" {
		route := h.getRoute(r.Host)

		// pick a target randomly
		revisionName := route.Status.Traffic[rand.Intn(len(route.Status.Traffic))].RevisionName

		// set headers accordingly
		r.Header.Add(activator.RevisionHeaderName, revisionName)
		r.Header.Add(activator.RevisionHeaderNamespace, route.Namespace)
	}

	h.nextHandler.ServeHTTP(w, r)
}
