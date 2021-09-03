/*
Copyright 2021 The Knative Authors

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
	"net/http"
	"sync"

	"go.uber.org/atomic"
	"go.uber.org/zap"
)

// ConcurrencyStateHandler tracks the in flight requests for the pod. When the requests
// drop to zero, it runs the `pause` function, and when requests scale up from zero, it
// runs the `resume` function. If either of `pause` or `resume` are not passed, it runs
// the respective local function(s). The local functions are the expected behavior; the
// function parameters are enabled primarily for testing purposes.
func ConcurrencyStateHandler(logger *zap.SugaredLogger, h http.Handler, pause, resume func()) http.HandlerFunc {
	logger.Info("Concurrency state tracking enabled")

	if pause == nil {
		pause = func() {}
	}

	if resume == nil {
		resume = func() {}
	}

	var (
		inFlight = atomic.NewInt64(0)
		paused   = true
		mux      sync.RWMutex
	)

	return func(w http.ResponseWriter, r *http.Request) {
		if inFlight.Inc() == 1 {
			func() {
				mux.Lock()
				defer mux.Unlock()
				// Keeping this state variable around as there is a race where we can
				// reach the pausing code below while another request arrives here. We
				// don't want to pause (and hence not resume) in that case.
				if paused {
					resume()
					paused = false
				}
			}()
		}

		defer func() {
			if inFlight.Dec() == 0 {
				mux.Lock()
				defer mux.Unlock()
				// We need to doublecheck this since another request can have reached the
				// handler meanwhile. We don't want to do anything in that case.
				if !paused && inFlight.Load() == 0 {
					pause()
					paused = true
				}
			}
		}()

		mux.RLock()
		defer mux.RUnlock()
		h.ServeHTTP(w, r)
	}
}
