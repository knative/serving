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
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	"go.uber.org/atomic"
	"go.uber.org/zap"
)

// ConcurrencyStateHandler tracks the in flight requests for the pod. When the requests
// drop to zero, it runs the `pause` function, and when requests scale up from zero, it
// runs the `resume` function. If either of `pause` or `resume` are not passed, it runs
// the respective local function(s). The local functions are the expected behavior; the
// function parameters are enabled primarily for testing purposes.
func ConcurrencyStateHandler(logger *zap.SugaredLogger, h http.Handler, pause, resume func() error) http.HandlerFunc {

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
						logger.Errorf("Error handling resume request: %v", err)
						handleStateRequestError(logger, pause)
					}
					paused = true
					logger.Debug("To-Zero request successfully processed")
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
			handleStateRequestError(logger, resume)
		}
		paused = false
		logger.Debug("From-Zero request successfully processed")
		mux.Unlock()

		h.ServeHTTP(w, r)
	}
}

// handleStateRequestError handles retry logic
func handleStateRequestError(logger *zap.SugaredLogger, requestHandler func() error) {
	var errReq error
	retryFunc := func() (bool, error) {
		errReq = requestHandler()
		return errReq == nil, nil
	}
	if err := wait.Poll(time.Millisecond*200, 15*time.Minute, retryFunc); err != nil {
		logger.Fatalf("Retry pause/resume request failed: %v", errReq)
		os.Exit(1)
	}
}

type ConcurrencyEndpoint struct {
	endpoint  string
	mountPath string
	token     atomic.Value
}

func NewConcurrencyEndpoint(e, m string) ConcurrencyEndpoint {
	c := ConcurrencyEndpoint{
		endpoint: os.Expand(e, func(s string) string {
			if s == "HOST_IP" {
				return os.Getenv("HOST_IP")
			}
			return "$" + s // to not change what the user provides
		}),
		mountPath: m,
	}
	c.RefreshToken()
	return c
}

func (c ConcurrencyEndpoint) Pause() error { return c.Request("pause") }

func (c ConcurrencyEndpoint) Resume() error { return c.Request("resume") }

func (c ConcurrencyEndpoint) Request(action string) error {
	bodyText := fmt.Sprintf(`{ "action": %q }`, action)
	body := bytes.NewBufferString(bodyText)
	req, err := http.NewRequest(http.MethodPost, c.endpoint, body)
	if err != nil {
		return fmt.Errorf("unable to create request: %w", err)
	}
	token := fmt.Sprint(c.token.Load())
	req.Header.Add("Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unable to post request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected 200 response, got: %d: %s", resp.StatusCode, resp.Status)
	}
	return nil
}

func (c *ConcurrencyEndpoint) RefreshToken() error {
	token, err := os.ReadFile(c.mountPath)
	if err != nil {
		return fmt.Errorf("could not read token: %w", err)
	}
	c.token.Store(string(token))
	return nil
}

func (c *ConcurrencyEndpoint) Endpoint() string {
	return c.endpoint
}
