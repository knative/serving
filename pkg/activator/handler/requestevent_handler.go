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

	"k8s.io/apimachinery/pkg/types"
)

// ReqEvent represents an incoming/finished request with a given key
type ReqEvent struct {
	Key       types.NamespacedName
	EventType ReqEventType
}

// ReqEventType specifies the type of event (In/Out)
type ReqEventType int

const (
	// ReqIn represents an incoming request
	ReqIn ReqEventType = iota
	// ReqOut represents a finished request
	ReqOut
)

// NewRequestEventHandler creates a handler that sends events
// about incoming/closed http connections to the given channel.
func NewRequestEventHandler(reqChan chan ReqEvent, next http.Handler) *RequestEventHandler {
	handler := &RequestEventHandler{
		nextHandler: next,
		ReqChan:     reqChan,
	}

	return handler
}

// RequestEventHandler sends events to the given channel.
type RequestEventHandler struct {
	nextHandler http.Handler
	ReqChan     chan ReqEvent
}

func (h *RequestEventHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	revisionKey := revIDFrom(r.Context())

	h.ReqChan <- ReqEvent{Key: revisionKey, EventType: ReqIn}
	defer func() {
		h.ReqChan <- ReqEvent{Key: revisionKey, EventType: ReqOut}
	}()
	h.nextHandler.ServeHTTP(w, r)
}
