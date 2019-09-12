/*
Copyright 2019 The Knative Authors

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

package health

import (
	"io"
	"net/http"
	"sync"
)

// State holds state about the current healthiness of the component.
type State struct {
	alive        bool
	shuttingDown bool
	mutex        sync.RWMutex

	drainCh        chan struct{}
	drainCompleted bool
}

// IsAlive returns whether or not the health server is in a known
// working state currently.
func (h *State) IsAlive() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	return h.alive
}

// IsShuttingDown returns whether or not the health server is currently
// shutting down.
func (h *State) IsShuttingDown() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	return h.shuttingDown
}

// setAlive updates the state to declare the service alive.
func (h *State) setAlive() {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.alive = true
	h.shuttingDown = false
}

// shutdown updates the state to declare the service shutting down.
func (h *State) shutdown() {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.alive = false
	h.shuttingDown = true
}

// drainFinish updates that we finished draining.
func (h *State) drainFinished() {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if !h.drainCompleted && h.drainCh != nil {
		close(h.drainCh)
	}

	h.drainCompleted = true

}

// HealthHandleFunc constructs a handler function that returns the current state of the
// health server. If isAggressive is false and prober has succeeded previously,
// the function return success without probing user-container again (until
// shutdown).
func (h *State) HealthHandleFunc(prober func() bool, isAggressive bool) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		h.HandleHealthProbe(prober, isAggressive, w, r)
	}
}

// HandleHealthProbe handles the request according to the current state of the
// health server. If isAggressive is false and prober has succeeded previously,
// the function return success without probing user-container again (until
// shutdown).
func (h *State) HandleHealthProbe(prober func() bool, isAggressive bool, w http.ResponseWriter, r *http.Request) {
	// Always return `queue` as body to indicate the response is from queue-proxy.
	// Please use the status code to determine whether the queue-proxy is alive.
	sendAlive := func() {
		io.WriteString(w, "queue")
	}

	sendNotAlive := func() {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, "queue")
	}

	switch {
	case !isAggressive && h.IsAlive():
		sendAlive()
	case h.IsShuttingDown():
		sendNotAlive()
	case prober != nil && !prober():
		sendNotAlive()
	default:
		h.setAlive()
		sendAlive()
	}
}

// DrainHandleFunc constructs a handle function that waits until the proxy server is shut down.
func (h *State) DrainHandleFunc() func(_ http.ResponseWriter, _ *http.Request) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if h.drainCh == nil {
		h.drainCh = make(chan struct{})
	}

	return func(_ http.ResponseWriter, _ *http.Request) {
		<-h.drainCh
	}
}

// Shutdown marks the proxy server as no ready and begins its shutdown process. This
// results in unblocking any connections waiting for drain.
func (h *State) Shutdown(drain func()) {
	h.shutdown()

	if drain != nil {
		drain()
	}

	h.drainFinished()
}
