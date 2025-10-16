/*
Copyright 2025 The Knative Authors

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
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"
)

const (
	// RegistrationEndpoint is the path on the activator where pod registration requests are sent
	RegistrationEndpoint = "/api/v1/pod-registration"

	// RegistrationTimeout is the maximum time to wait for a registration request to complete
	RegistrationTimeout = 2 * time.Second

	// EventTypeStartup indicates the pod is starting up
	EventTypeStartup = "startup"

	// EventTypeReady indicates the pod is ready to serve traffic
	EventTypeReady = "ready"
)

// registrationClient is a shared HTTP client for pod registration requests.
// Using a shared client is more efficient than creating a new client for each request.
// The transport is configured for connection pooling and keep-alives to reduce overhead.
var registrationClient = &http.Client{
	Timeout: RegistrationTimeout,
	Transport: &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
		DisableKeepAlives:   false, // Enable connection reuse
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	},
}

// PodRegistrationRequest is the JSON payload sent to the activator for pod registration
type PodRegistrationRequest struct {
	PodName   string `json:"pod_name"`
	PodIP     string `json:"pod_ip"`
	Namespace string `json:"namespace"`
	Revision  string `json:"revision"`
	EventType string `json:"event_type"`
	Timestamp string `json:"timestamp"`
}

// RegisterPodWithActivator sends a pod registration request to the activator asynchronously
// activatorServiceURL should be the full base URL (e.g., "http://activator-service.knative-serving.svc.cluster.local:80")
// If activatorServiceURL is empty, this is a no-op
// Failures are logged at debug level and never block the caller
func RegisterPodWithActivator(
	activatorServiceURL string,
	eventType string,
	podName string,
	podIP string,
	namespace string,
	revision string,
	logger *zap.SugaredLogger,
) {
	if activatorServiceURL == "" {
		return
	}

	go func() {
		registerPodSync(activatorServiceURL, eventType, podName, podIP, namespace, revision, logger)
	}()
}

// registerPodSync performs the actual registration request synchronously
func registerPodSync(
	activatorServiceURL string,
	eventType string,
	podName string,
	podIP string,
	namespace string,
	revision string,
	logger *zap.SugaredLogger,
) {
	ctx, cancel := context.WithTimeout(context.Background(), RegistrationTimeout)
	defer cancel()

	req := &PodRegistrationRequest{
		PodName:   podName,
		PodIP:     podIP,
		Namespace: namespace,
		Revision:  revision,
		EventType: eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	body, err := json.Marshal(req)
	if err != nil {
		if logger != nil {
			logger.Errorw("Failed to marshal pod registration request",
				"event", eventType,
				"pod", podName,
				"error", err)
		}
		return
	}

	url := activatorServiceURL + RegistrationEndpoint
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		if logger != nil {
			logger.Errorw("Failed to create pod registration request",
				"event", eventType,
				"pod", podName,
				"url", url,
				"error", err)
		}
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "knative-queue-proxy")

	// Use shared HTTP client for efficiency - avoids creating new transport for each request
	resp, err := registrationClient.Do(httpReq)
	if err != nil {
		if logger != nil {
			// Check if it's a timeout error specifically
			if errors.Is(err, context.DeadlineExceeded) {
				logger.Warnw("Pod registration request timeout",
					"event", eventType,
					"pod", podName,
					"url", url,
					"timeout", RegistrationTimeout)
			} else {
				logger.Errorw("Pod registration request failed",
					"event", eventType,
					"pod", podName,
					"url", url,
					"error", err)
			}
		}
		return
	}
	defer resp.Body.Close()

	// Read response body for logging in case of errors
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if logger != nil {
			logger.Debugw("Pod registration successful",
				"event", eventType,
				"pod", podName,
				"status", resp.StatusCode)
		}
		return
	}

	// Log non-success responses with appropriate levels
	if logger != nil {
		if resp.StatusCode >= 500 {
			logger.Errorw("Pod registration server error (5xx)",
				"event", eventType,
				"pod", podName,
				"status", resp.StatusCode,
				"response", string(respBody))
		} else if resp.StatusCode >= 400 {
			logger.Warnw("Pod registration client error (4xx)",
				"event", eventType,
				"pod", podName,
				"status", resp.StatusCode,
				"response", string(respBody))
		} else {
			logger.Warnw("Pod registration request failed with unexpected status",
				"event", eventType,
				"pod", podName,
				"status", resp.StatusCode,
				"response", string(respBody))
		}
	}
}
