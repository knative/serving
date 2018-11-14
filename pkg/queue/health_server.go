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
package queue

import (
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	// Number of seconds the /quitquitquit handler should wait before
	// returning.  The purpose is to keep the container alive a little
	// bit longer, that it doesn't go away until the pod is truly
	// removed from service.
	quitSleepSecs = 20
)

// HealthServer registers whether a PreStop hook has been called.
type HealthServer struct {
	alive bool
	mutex sync.RWMutex
}

// IsAlive returns true until a PreStop hook has been called.
func (h *HealthServer) IsAlive() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.alive
}

// Kill marks that a PreStop hook has been called.
func (h *HealthServer) Kill() {
	h.mutex.Lock()
	h.alive = false
	h.mutex.Unlock()
}

// HealthHandler is used for readinessProbe/livenessCheck of
// queue-proxy.
func (h *HealthServer) HealthHandler(w http.ResponseWriter, r *http.Request) {
	if h.IsAlive() {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "alive: true")
	} else {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "alive: false")
	}
}

// QuitHandler is used for preStop hook of queue-proxy. It:
// - marks the service as not ready, so that requests will no longer
//   be routed to it,
// - adds a small delay, so that the container doesn't get killed at
//   the same time the pod is marked for removal.
func (h *HealthServer) QuitHandler(w http.ResponseWriter, r *http.Request) {
	// First, we want to mark the container as not ready, so that even
	// if the pod removal (from service) isn't yet effective, the
	// readinessCheck will still prevent traffic to be routed to this
	// pod.
	h.Kill()
	// However, since both readinessCheck and pod removal from service
	// is eventually consistent, we add here a small delay to have the
	// container stay alive a little bit longer after.  We still have
	// no guarantee that container termination is done only after
	// removal from service is effective, but this has been showed to
	// alleviate the issue.
	time.Sleep(quitSleepSecs * time.Second)
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "alive: false")
}
