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

	type req struct {
		w http.ResponseWriter
		r *http.Request

		done chan struct{}
	}

	reqCh := make(chan req)
	doneCh := make(chan struct{})
	go func() {
		inFlight := 0

		// This loop is entirely synchronous, so there's no cleverness needed in
		// ensuring open and close dont run at the same time etc. Only the
		// delegated ServeHTTP is done in a goroutine.
		for {
			select {
			case <-doneCh:
				inFlight--
				if inFlight == 0 {
					logger.Info("Requests dropped to zero ...")
					pause()
				}

			case r := <-reqCh:
				inFlight++
				if inFlight == 1 {
					logger.Info("Requests increased from zero ...")
					resume()
				}

				go func(r req) {
					h.ServeHTTP(r.w, r.r)
					close(r.done) // Return from ServeHTTP
					doneCh <- struct{}{}
				}(r)
			}
		}
	}()

	return func(w http.ResponseWriter, r *http.Request) {
		done := make(chan struct{})
		reqCh <- req{w, r, done}
		// Block till we've processed the request
		<-done
	}
}
