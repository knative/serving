/*
Copyright 2018 Google LLC

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
package activator

import (
	"fmt"
	"sync"
)

var shuttingDownError = activationResult{
	endpoint: Endpoint{},
	status:   Status(500),
	err:      fmt.Errorf("Activator shutting down."),
}

type activationResult struct {
	endpoint Endpoint
	status   Status
	err      error
}

type DedupingActivator struct {
	mux             sync.Mutex
	pendingRequests map[revisionId][]chan activationResult
	activator       Activator
	shutdown        bool
}

func NewDedupingActivator(a Activator) Activator {
	return &DedupingActivator{
		pendingRequests: make(map[revisionId][]chan activationResult),
		activator:       a,
	}
}

func (a *DedupingActivator) ActiveEndpoint(namespace, name string) (Endpoint, Status, error) {
	id := revisionId{namespace: namespace, name: name}
	ch := make(chan activationResult, 1)
	a.dedupe(id, ch)
	result := <-ch
	return result.endpoint, result.status, result.err
}

func (a *DedupingActivator) Shutdown() {
	a.activator.Shutdown()
	a.mux.Lock()
	defer a.mux.Unlock()
	a.shutdown = true
	for _, reqs := range a.pendingRequests {
		for _, ch := range reqs {
			ch <- shuttingDownError
		}
	}
}

func (a *DedupingActivator) dedupe(id revisionId, ch chan activationResult) {
	a.mux.Lock()
	defer a.mux.Unlock()
	if a.shutdown {
		ch <- shuttingDownError
		return
	}
	if reqs, ok := a.pendingRequests[id]; ok {
		a.pendingRequests[id] = append(reqs, ch)
	} else {
		a.pendingRequests[id] = []chan activationResult{ch}
		go a.activate(id)
	}
}

func (a *DedupingActivator) activate(id revisionId) {
	endpoint, status, err := a.activator.ActiveEndpoint(id.namespace, id.name)
	a.mux.Lock()
	defer a.mux.Unlock()
	result := activationResult{
		endpoint: endpoint,
		status:   status,
		err:      err,
	}
	if reqs, ok := a.pendingRequests[id]; ok {
		delete(a.pendingRequests, id)
		for _, ch := range reqs {
			ch <- result
		}
	}
}
