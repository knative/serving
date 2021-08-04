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

func ConcurrencyStateHandler(logger *zap.SugaredLogger, h http.Handler) http.HandlerFunc {
	logger.Info("Concurrency state tracking enabled")

	type req struct {
		w http.ResponseWriter
		r *http.Request

		done chan struct{}
	}

	reqCh := make(chan req)
	doneCh := make(chan struct{})
	go func() {
		inFlight := 0

		// this loop is entirely synchronous, so there's no cleverness needed in
		// ensuring open and close dont run at the same time etc. Only the
		// delegated ServeHTTP is done in a goroutine.
		for {
			select {
			case <-doneCh:
				inFlight--
				if inFlight == 0 {
					logger.Info("Pod Freezer: Pausing...")
					// TODO: endpoint call
				}

			case r := <-reqCh:
				inFlight++
				if inFlight == 1 {
					logger.Info("Pod Freezer: Resuming...")
					// TODO: endpoint call
				}

				go func(r req) {
					h.ServeHTTP(r.w, r.r)
					close(r.done) // return from ServeHTTP.
					doneCh <- struct{}{}
				}(r)
			}
		}
	}()

	return func(w http.ResponseWriter, r *http.Request) {
		done := make(chan struct{})
		reqCh <- req{w, r, done}
		// block till we're processed
		<-done
	}
}
