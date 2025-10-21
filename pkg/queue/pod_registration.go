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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

const (
	// RegistrationEndpoint is the path on the activator where pod registration requests are sent
	RegistrationEndpoint = "/api/v1/pod-registration"

	// RegistrationTimeout is the maximum time to wait for a registration request to complete
	RegistrationTimeout = 2 * time.Second

	// EventStartup indicates the pod is starting up
	EventStartup = "startup"

	// EventReady indicates the pod is ready to serve traffic
	EventReady = "ready"

	// EventNotReady indicates the pod's readiness probe failed
	EventNotReady = "not-ready"

	// EventDraining indicates the pod is shutting down gracefully
	EventDraining = "draining"
)

// Prometheus metrics for pod registration monitoring
var (
	// registrationAttempts tracks the number of pod registration attempts
	// Labels: result, event_type, namespace, revision, activator_url
	registrationAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pod_registration_attempts_total",
			Help: "Total number of pod registration attempts to activator",
		},
		[]string{"result", "event_type", "namespace", "revision", "activator_url"},
	)

	// registrationLatency tracks the time taken to register a pod
	// Labels: event_type, namespace, revision, activator_url
	registrationLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pod_registration_duration_seconds",
			Help:    "Time taken to complete pod registration request",
			Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"event_type", "namespace", "revision", "activator_url"},
	)
)

// registrationClient is a shared HTTP client for pod registration requests.
// Using a shared client is more efficient than creating a new client for each request.
// DisableKeepAlives is set to true to prevent the activator from holding thousands of idle connections.
// In clusters with many pods, keeping connections alive would require the activator to maintain:
// - ~5000+ idle TCP connections (one per queue-proxy)
// - ~5000+ goroutines managing those connections
// - ~5000+ kernel conntrack entries
// Since pod status updates are infrequent (typically just 2 requests per pod lifecycle),
// the cost of new TCP connections is acceptable vs. the resource burden on the activator.
var registrationClient = &http.Client{
	Timeout: RegistrationTimeout,
	Transport: &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
		DisableKeepAlives:   true, // Disable connection reuse to prevent activator resource exhaustion
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
// Failures are logged but never block the caller - this is a best-effort optimization
// The 2-second timeout provides natural backpressure if the activator is unavailable
// Deduplication is handled at the caller level via state-based transition detection
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
// Records metrics about registration attempts and latency
func registerPodSync(
	activatorServiceURL string,
	eventType string,
	podName string,
	podIP string,
	namespace string,
	revision string,
	logger *zap.SugaredLogger,
) {
	startTime := time.Now()
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
		// Record metric for marshal failure (should never happen but good for completeness)
		registrationAttempts.WithLabelValues("marshal_error", eventType, namespace, revision, activatorServiceURL).Inc()
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
		registrationLatency.WithLabelValues(eventType, namespace, revision, activatorServiceURL).Observe(time.Since(startTime).Seconds())

		// Check if it's a timeout error specifically and record appropriate metric
		if errors.Is(err, context.DeadlineExceeded) {
			registrationAttempts.WithLabelValues("timeout", eventType, namespace, revision, activatorServiceURL).Inc()
			if logger != nil {
				logger.Warnw("Pod registration request timeout",
					"event", eventType,
					"pod", podName,
					"url", url,
					"timeout", RegistrationTimeout)
			}
		} else {
			registrationAttempts.WithLabelValues("network_error", eventType, namespace, revision, activatorServiceURL).Inc()
			if logger != nil {
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
		// Record metrics
		registrationAttempts.WithLabelValues("success", eventType, namespace, revision, activatorServiceURL).Inc()
		registrationLatency.WithLabelValues(eventType, namespace, revision, activatorServiceURL).Observe(time.Since(startTime).Seconds())
		if logger != nil {
			logger.Infow("Pod registration successful",
				"event", eventType,
				"pod", podName,
				"status", resp.StatusCode)
		}
		return
	}

	// Record failed registration metrics
	registrationLatency.WithLabelValues(eventType, namespace, revision, activatorServiceURL).Observe(time.Since(startTime).Seconds())

	// Log non-success responses with appropriate levels and record metrics
	if logger != nil {
		if resp.StatusCode >= 500 {
			registrationAttempts.WithLabelValues("5xx", eventType, namespace, revision, activatorServiceURL).Inc()
			logger.Errorw("Pod registration server error (5xx)",
				"event", eventType,
				"pod", podName,
				"status", resp.StatusCode,
				"response", string(respBody))
		} else if resp.StatusCode >= 400 {
			registrationAttempts.WithLabelValues("4xx", eventType, namespace, revision, activatorServiceURL).Inc()
			logger.Warnw("Pod registration client error (4xx)",
				"event", eventType,
				"pod", podName,
				"status", resp.StatusCode,
				"response", string(respBody))
		} else {
			registrationAttempts.WithLabelValues("unexpected", eventType, namespace, revision, activatorServiceURL).Inc()
			logger.Warnw("Pod registration request failed with unexpected status",
				"event", eventType,
				"pod", podName,
				"status", resp.StatusCode,
				"response", string(respBody))
		}
	} else {
		// Record metrics even if logger is nil
		if resp.StatusCode >= 500 {
			registrationAttempts.WithLabelValues("5xx", eventType, namespace, revision, activatorServiceURL).Inc()
		} else if resp.StatusCode >= 400 {
			registrationAttempts.WithLabelValues("4xx", eventType, namespace, revision, activatorServiceURL).Inc()
		} else {
			registrationAttempts.WithLabelValues("unexpected", eventType, namespace, revision, activatorServiceURL).Inc()
		}
	}
}
