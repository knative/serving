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
	"net/http"

	"knative.dev/serving/pkg/activator/util"
	"knative.dev/serving/pkg/network"
)

// NewRequestEventHandler creates a handler that sends events
// about incoming/closed http connections to the given channel.
func NewRequestEventHandler(reqChan chan network.ReqEvent, next http.Handler) *RequestEventHandler {
	handler := &RequestEventHandler{
		nextHandler: next,
		reqChan:     reqChan,
	}

	return handler
}

// RequestEventHandler sends events to the given channel.
type RequestEventHandler struct {
	nextHandler http.Handler
	reqChan     chan network.ReqEvent
}

func (h *RequestEventHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	revisionKey := util.RevIDFrom(r.Context())

	h.reqChan <- network.ReqEvent{Key: revisionKey, Type: network.ReqIn}
	defer func() {
		h.reqChan <- network.ReqEvent{Key: revisionKey, Type: network.ReqOut}
	}()
	h.nextHandler.ServeHTTP(w, r)
}
