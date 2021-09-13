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
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"go.uber.org/atomic"
	"go.uber.org/zap"
)

//nolint:gosec // Filepath, not hardcoded credentials
const ConcurrencyStateTokenVolumeMountPath = "/var/run/secrets/tokens"

// ConcurrencyStateHandler tracks the in flight requests for the pod. When the requests
// drop to zero, it runs the `pause` function, and when requests scale up from zero, it
// runs the `resume` function. If either of `pause` or `resume` are not passed, it runs
// the respective local function(s). The local functions are the expected behavior; the
// function parameters are enabled primarily for testing purposes.
func ConcurrencyStateHandler(logger *zap.SugaredLogger, h http.Handler, pause, resume func() error) http.HandlerFunc {
	logger.Info("Concurrency state tracking enabled")

	var (
		inFlight = atomic.NewInt64(0)
		paused   = true
		mux      sync.RWMutex
	)

	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if inFlight.Dec() == 0 {
				mux.Lock()
				defer mux.Unlock()
				// We need to doublecheck this since another request can have reached the
				// handler meanwhile. We don't want to do anything in that case.
				if !paused && inFlight.Load() == 0 {
					logger.Info("Requests dropped to zero")
					if err := pause(); err != nil {
						panic(err)
					}
					paused = true
					logger.Info("To-Zero request successfully processed")
				}
			}
		}()

		inFlight.Inc()

		mux.RLock()
		if !paused {
			// General stable-state case.
			defer mux.RUnlock()
			h.ServeHTTP(w, r)
			return
		}
		mux.RUnlock()
		mux.Lock()
		if !paused { // doubly-checked locking
			// Another request raced us and resumed already.
			defer mux.Unlock()
			h.ServeHTTP(w, r)
			return
		}

		logger.Info("Requests increased from zero")
		if err := resume(); err != nil {
			logger.Errorf("Error handling resume request: %v", err)
			panic(err)
		}
		paused = false
		logger.Info("From-Zero request successfully processed")
		mux.Unlock()

		h.ServeHTTP(w, r)
	}
}

// ConcurrencyState Request sends to the concurrency state endpoint
func ConcurrencyStateRequest(endpoint string, bodyText []byte) func() error {
	return func() error {
		body := bytes.NewBuffer(bodyText)
		req, err := http.NewRequest(http.MethodPost, endpoint, body)
		if err != nil {
			return fmt.Errorf("unable to create request: %w", err)
		}
		req.Header.Add("Token", "nil") // TODO: use serviceaccountToken from projected volume
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("unable to post request: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected 200 response, got: %d: %s", resp.StatusCode, resp.Status)
		}
		return nil
	}
}

func CreateConcurrencyStateMessageBody(action ConcurrencyStateMessageBody) ([]byte, error) {
	return json.Marshal(action)
}

type ConcurrencyStateMessageBody struct {
	Action string `json:"action"`
}
