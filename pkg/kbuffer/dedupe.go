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

package kbuffer

import (
	"fmt"
	"net/http"
	"sync"
)

var shuttingDownError = ActivationResult{
	Endpoint: Endpoint{},
	Status:   http.StatusInternalServerError,
	Error:    fmt.Errorf("kbuffer shutting down"),
}

var _ KBuffer = (*dedupingKBuffer)(nil)

type dedupingKBuffer struct {
	mux             sync.Mutex
	pendingRequests map[revisionID][]chan ActivationResult
	kbuffer       KBuffer
	shutdown        bool
}

// NewDedupingKBuffer creates a KBuffer that deduplicates
// activations requests for the same revision id and namespace.
func NewDedupingKBuffer(kb KBuffer) KBuffer {
	return &dedupingKBuffer{
		pendingRequests: make(map[revisionID][]chan ActivationResult),
		kbuffer:         kb,
	}
}

func (a *dedupingKBuffer) ActiveEndpoint(namespace, name string) ActivationResult {
	id := revisionID{namespace: namespace, name: name}
	ch := make(chan ActivationResult, 1)
	a.dedupe(id, ch)
	result := <-ch
	return result
}

func (a *dedupingKBuffer) Shutdown() {
	a.kbuffer.Shutdown()
	a.mux.Lock()
	defer a.mux.Unlock()
	a.shutdown = true
	for _, reqs := range a.pendingRequests {
		for _, ch := range reqs {
			ch <- shuttingDownError
		}
	}
}

func (a *dedupingKBuffer) dedupe(id revisionID, ch chan ActivationResult) {
	a.mux.Lock()
	defer a.mux.Unlock()
	if a.shutdown {
		ch <- shuttingDownError
		return
	}
	if reqs, ok := a.pendingRequests[id]; ok {
		a.pendingRequests[id] = append(reqs, ch)
	} else {
		a.pendingRequests[id] = []chan ActivationResult{ch}
		go a.activate(id)
	}
}

func (a *dedupingKBuffer) activate(id revisionID) {
	result := a.kbuffer.ActiveEndpoint(id.namespace, id.name)
	a.mux.Lock()
	defer a.mux.Unlock()
	if reqs, ok := a.pendingRequests[id]; ok {
		delete(a.pendingRequests, id)
		for _, ch := range reqs {
			ch <- result
		}
	}
}
