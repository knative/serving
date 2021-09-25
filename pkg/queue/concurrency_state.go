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

	"go.uber.org/atomic"
	"go.uber.org/zap"
)

//nolint:gosec // Filepath, not hardcoded credentials
const ConcurrencyStateTokenVolumeMountPath = "/var/run/secrets/tokens"
const FreezeMaxRetryTimes = 3 // If pause/resume failed 3 times, it should kill qp and delete user-container force

// error code for pause/resume operation
const (
	noError                        = 0 // everything works
	internalError                  = 1 // some internal errors happen, like json decode failed, should try again
	responseStatusConflictError    = 2 // like request pause when the container is in pause state, should forget
	responseExecError              = 3 // the command exec failed in runtime level, should try again
)

// ConcurrencyStateHandler tracks the in flight requests for the pod. When the requests
// drop to zero, it runs the `pause` function, and when requests scale up from zero, it
// runs the `resume` function. If either of `pause` or `resume` are not passed, it runs
// the respective local function(s). The local functions are the expected behavior; the
// function parameters are enabled primarily for testing purposes.
func ConcurrencyStateHandler(logger *zap.SugaredLogger, h http.Handler,
	pause, resume, delete func() (int8, error)) http.HandlerFunc {

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
					if errCode, err := pause(); errCode != noError {
						logger.Errorf("Error handling resume request: %v", err)
						handleStateRequestError(errCode, pause, delete)
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
		if errCode, err := resume(); errCode != noError {
			logger.Errorf("Error handling resume request: %v", err)
			handleStateRequestError(errCode, resume, delete)
		}
		paused = false
		logger.Debug("From-Zero request successfully processed")
		mux.Unlock()

		h.ServeHTTP(w, r)
	}
}

// handleStateRequestError handles different error code
func handleStateRequestError(errCode int8, requestHandler, deleteFunc func() (int8, error)) {
	if errCode == responseStatusConflictError {
		// nothing should be done, just ignore
	}
	if errCode == internalError {
		os.Exit(1)
	}
	if errCode == responseExecError {
		failedTimes := 0
		for failedTimes < FreezeMaxRetryTimes {
			errCode, _ := requestHandler()
			if errCode == responseExecError {
				failedTimes++
				time.Sleep(time.Millisecond * 200)
			}
			if errCode == internalError {
				os.Exit(1)
			}
			if errCode == responseStatusConflictError || errCode == noError {
				break
			}
		}
		if failedTimes >= FreezeMaxRetryTimes {
			// Relaunch this pod, the way is: the runtime will delete all containers of this pod
			errCode, error := deleteFunc()
			// if the QP is deleted, this will not be executed
			for error != nil {
				// the error code type here could be: internalError, responseExecError(failed to exec delete command)
				if errCode == internalError {
					os.Exit(1)
				} else {
					_, error = deleteFunc()
				}
			}
		}
	}
}

// concurrencyStateRequest sends a request to the concurrency state endpoint.
func concurrencyStateRequest(endpoint string, action string) func() (int8, error) {
	return func() (int8, error) {
		bodyText := fmt.Sprintf(`{ "action": %q }`, action)
		body := bytes.NewBufferString(bodyText)
		req, err := http.NewRequest(http.MethodPost, endpoint, body)
		if err != nil {
			return internalError, fmt.Errorf("unable to create request: %w", err)
		}
		req.Header.Add("Token", "nil") // TODO: use serviceaccountToken from projected volume
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return internalError, fmt.Errorf("unable to post request: %w", err)
		}
		if resp.StatusCode == http.StatusConflict {
			if action == "pause" {
				return responseStatusConflictError, fmt.Errorf("expected container status is in running state, but actually not")
			} else {
				return responseStatusConflictError, fmt.Errorf("expected container status is in paused state, but actually not")
			}
		}
		if resp.StatusCode != http.StatusOK {
			return responseExecError, fmt.Errorf("expected 200 response, got: %d: %s", resp.StatusCode, resp.Status)
		}
		return noError, nil
	}
}

// Pause sends a pause request to the concurrency state endpoint.
func Pause(endpoint string) func() (int8, error) {
	return concurrencyStateRequest(endpoint, "pause")
}

// Resume sends a resume request to the concurrency state endpoint.
func Resume(endpoint string) func() (int8, error) {
	return concurrencyStateRequest(endpoint, "resume")
}

// RelaunchUserContainer send a force-delete request to the concurrency state endpoint.
func RelaunchUserContainer(endpoint string) func() (int8, error) {
	return concurrencyStateRequest(endpoint, "delete-pod")
}