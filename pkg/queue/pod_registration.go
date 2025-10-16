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
	"math"
	"net"
	"net/http"
	"sync"
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

	// EventTypeStartup indicates the pod is starting up
	EventTypeStartup = "startup"

	// EventTypeReady indicates the pod is ready to serve traffic
	EventTypeReady = "ready"

	// Circuit breaking constants for resilience when activator is unavailable
	// RegistrationInitialBackoff is the initial backoff duration on first failure (100ms)
	RegistrationInitialBackoff = 100 * time.Millisecond

	// RegistrationMaxBackoff is the maximum backoff duration (60 seconds)
	RegistrationMaxBackoff = 60 * time.Second

	// RegistrationBackoffMultiplier is the exponential multiplier for backoff (2x)
	RegistrationBackoffMultiplier = 2.0

	// RegistrationDeduplicationWindow is how long to ignore duplicate registration attempts (5 seconds)
	RegistrationDeduplicationWindow = 5 * time.Second

	// RegistrationDuplicateCleanupInterval is how often to clean stale deduplication entries (30 seconds)
	RegistrationDuplicateCleanupInterval = 30 * time.Second
)

// Prometheus metrics for pod registration monitoring
var (
	// registrationAttempts tracks the number of pod registration attempts
	// Labels: result (success, timeout, network_error, 4xx, 5xx), event_type (startup, ready)
	registrationAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pod_registration_attempts_total",
			Help: "Total number of pod registration attempts to activator",
		},
		[]string{"result", "event_type"},
	)

	// registrationLatency tracks the time taken to register a pod
	// Labels: event_type (startup, ready)
	registrationLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "pod_registration_duration_seconds",
			Help: "Time taken to complete pod registration request",
			Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"event_type"},
	)

	// circuitBreakerFailures tracks the number of consecutive failures per activator
	// This is a gauge that shows current failure count for each activator URL
	// Labels: activator_url
	circuitBreakerFailures = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pod_registration_circuit_breaker_failures",
			Help: "Current number of consecutive registration failures for activator",
		},
		[]string{"activator_url"},
	)

	// deduplicationCacheSize tracks the size of the active deduplication cache
	deduplicationCacheSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "pod_registration_deduplication_cache_size",
			Help: "Number of entries in the registration deduplication cache",
		},
	)

	// registrationSkipped tracks the number of registration attempts skipped due to circuit breaking or deduplication
	// Labels: reason (duplicate, circuit_breaking), event_type (startup, ready)
	registrationSkipped = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pod_registration_skipped_total",
			Help: "Number of registration attempts skipped due to circuit breaking or deduplication",
		},
		[]string{"reason", "event_type"},
	)
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

// registrationBackoffTracker tracks failed registration attempts per activator URL
// for implementing circuit breaking with exponential backoff
type registrationBackoffTracker struct {
	mu        sync.Mutex
	attempts  map[string]int       // key: activator URL, value: consecutive failure count
	lastTried map[string]time.Time  // key: activator URL, value: time of last attempt
}

// registrationDeduplicationCache tracks recent registration attempts to avoid duplicates
type registrationDeduplicationCache struct {
	mu    sync.Mutex
	cache map[string]time.Time // key: "podIP:eventType", value: last seen time
}

// Global circuit breaker and deduplication instances
var (
	backoffTracker = &registrationBackoffTracker{
		attempts:  make(map[string]int),
		lastTried: make(map[string]time.Time),
	}
	deduplicationCache = &registrationDeduplicationCache{
		cache: make(map[string]time.Time),
	}
)

// getBackoffDuration calculates exponential backoff for a given attempt count
func getBackoffDuration(attemptCount int) time.Duration {
	if attemptCount <= 0 {
		return 0
	}
	// exponential backoff: initial * multiplier^(attempts-1)
	// Cap at maxBackoff to prevent overflow
	backoffSeconds := RegistrationInitialBackoff.Seconds() * math.Pow(RegistrationBackoffMultiplier, float64(attemptCount-1))
	maxSeconds := RegistrationMaxBackoff.Seconds()
	if backoffSeconds > maxSeconds {
		backoffSeconds = maxSeconds
	}
	return time.Duration(backoffSeconds * 1000) * time.Millisecond
}

// shouldSkipRegistration checks if we should skip this registration due to backoff or deduplication
// Returns true if registration should be skipped (with optional reason)
func shouldSkipRegistration(activatorURL string, podIP string, eventType string, logger *zap.SugaredLogger) bool {
	// Check deduplication first - most common case
	if isDuplicate(podIP, eventType) {
		if logger != nil {
			logger.Debugw("Skipping duplicate registration attempt",
				"pod-ip", podIP,
				"event-type", eventType,
				"activator-url", activatorURL)
		}
		registrationSkipped.WithLabelValues("duplicate", eventType).Inc()
		return true
	}

	// Check circuit breaker backoff
	backoffTracker.mu.Lock()
	defer backoffTracker.mu.Unlock()

	attemptCount, ok := backoffTracker.attempts[activatorURL]
	if !ok || attemptCount == 0 {
		// No previous failures, allow registration
		return false
	}

	lastTried := backoffTracker.lastTried[activatorURL]
	backoffDuration := getBackoffDuration(attemptCount)
	timeSinceLastTry := time.Since(lastTried)

	if timeSinceLastTry < backoffDuration {
		if logger != nil {
			logger.Debugw("Skipping registration due to circuit breaker backoff",
				"pod-ip", podIP,
				"activator-url", activatorURL,
				"attempt-count", attemptCount,
				"backoff-duration", backoffDuration,
				"time-since-last-try", timeSinceLastTry)
		}
		registrationSkipped.WithLabelValues("circuit_breaking", eventType).Inc()
		return true
	}

	// Backoff period has expired, allow retry
	return false
}

// isDuplicate checks if this is a duplicate registration within the deduplication window
func isDuplicate(podIP string, eventType string) bool {
	deduplicationCache.mu.Lock()
	defer deduplicationCache.mu.Unlock()

	key := podIP + ":" + eventType
	lastSeen, exists := deduplicationCache.cache[key]
	if !exists {
		// First time seeing this, record it
		deduplicationCache.cache[key] = time.Now()
		return false
	}

	// Check if still within deduplication window
	if time.Since(lastSeen) < RegistrationDeduplicationWindow {
		return true
	}

	// Outside window, treat as new registration
	deduplicationCache.cache[key] = time.Now()
	return false
}

// recordRegistrationAttempt records a registration attempt for circuit breaking
func recordRegistrationAttempt(activatorURL string, success bool) {
	backoffTracker.mu.Lock()
	defer backoffTracker.mu.Unlock()

	if success {
		// Reset on success
		backoffTracker.attempts[activatorURL] = 0
		delete(backoffTracker.lastTried, activatorURL)
		// Update gauge to reflect reset
		circuitBreakerFailures.WithLabelValues(activatorURL).Set(0)
	} else {
		// Increment on failure
		backoffTracker.attempts[activatorURL]++
		backoffTracker.lastTried[activatorURL] = time.Now()
		// Update gauge with new failure count
		circuitBreakerFailures.WithLabelValues(activatorURL).Set(float64(backoffTracker.attempts[activatorURL]))
	}
}

// cleanupDeduplicationCache removes stale entries from the deduplication cache
// This is called periodically to prevent unbounded memory growth
func cleanupDeduplicationCache() {
	deduplicationCache.mu.Lock()
	defer deduplicationCache.mu.Unlock()

	now := time.Now()
	for key, lastSeen := range deduplicationCache.cache {
		if now.Sub(lastSeen) > RegistrationDuplicateCleanupInterval {
			delete(deduplicationCache.cache, key)
		}
	}
}

// init starts background goroutines to clean up stale deduplication entries and update metrics
func init() {
	go func() {
		ticker := time.NewTicker(RegistrationDuplicateCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			cleanupDeduplicationCache()
			// Update deduplication cache size metric
			deduplicationCache.mu.Lock()
			deduplicationCacheSize.Set(float64(len(deduplicationCache.cache)))
			deduplicationCache.mu.Unlock()
		}
	}()
}

// ResetDeduplicationCacheForTesting resets the deduplication cache
// This is only exported for testing purposes and should not be used in production
func ResetDeduplicationCacheForTesting() {
	deduplicationCache.mu.Lock()
	defer deduplicationCache.mu.Unlock()
	deduplicationCache.cache = make(map[string]time.Time)
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
// Implements circuit breaking with exponential backoff to prevent thundering herd if activator is down
// Implements request deduplication to prevent duplicate registrations from pod restarts
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

	// Check if this should be skipped due to deduplication or circuit breaker
	if shouldSkipRegistration(activatorServiceURL, podIP, eventType, logger) {
		return
	}

	go func() {
		registerPodSync(activatorServiceURL, eventType, podName, podIP, namespace, revision, logger)
	}()
}

// registerPodSync performs the actual registration request synchronously
// Handles circuit breaking by recording failures for exponential backoff
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
		// Record failure for circuit breaker
		recordRegistrationAttempt(activatorServiceURL, false)
		registrationLatency.WithLabelValues(eventType).Observe(time.Since(startTime).Seconds())

		// Check if it's a timeout error specifically and record appropriate metric
		if errors.Is(err, context.DeadlineExceeded) {
			registrationAttempts.WithLabelValues("timeout", eventType).Inc()
			if logger != nil {
				logger.Warnw("Pod registration request timeout",
					"event", eventType,
					"pod", podName,
					"url", url,
					"timeout", RegistrationTimeout)
			}
		} else {
			registrationAttempts.WithLabelValues("network_error", eventType).Inc()
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
		// Record successful registration for circuit breaker reset
		recordRegistrationAttempt(activatorServiceURL, true)
		// Record metrics
		registrationAttempts.WithLabelValues("success", eventType).Inc()
		registrationLatency.WithLabelValues(eventType).Observe(time.Since(startTime).Seconds())
		if logger != nil {
			logger.Debugw("Pod registration successful",
				"event", eventType,
				"pod", podName,
				"status", resp.StatusCode)
		}
		return
	}

	// Record failed registration for circuit breaker exponential backoff
	recordRegistrationAttempt(activatorServiceURL, false)
	registrationLatency.WithLabelValues(eventType).Observe(time.Since(startTime).Seconds())

	// Log non-success responses with appropriate levels and record metrics
	if logger != nil {
		if resp.StatusCode >= 500 {
			registrationAttempts.WithLabelValues("5xx", eventType).Inc()
			logger.Errorw("Pod registration server error (5xx)",
				"event", eventType,
				"pod", podName,
				"status", resp.StatusCode,
				"response", string(respBody))
		} else if resp.StatusCode >= 400 {
			registrationAttempts.WithLabelValues("4xx", eventType).Inc()
			logger.Warnw("Pod registration client error (4xx)",
				"event", eventType,
				"pod", podName,
				"status", resp.StatusCode,
				"response", string(respBody))
		} else {
			registrationAttempts.WithLabelValues("unexpected", eventType).Inc()
			logger.Warnw("Pod registration request failed with unexpected status",
				"event", eventType,
				"pod", podName,
				"status", resp.StatusCode,
				"response", string(respBody))
		}
	} else {
		// Record metrics even if logger is nil
		if resp.StatusCode >= 500 {
			registrationAttempts.WithLabelValues("5xx", eventType).Inc()
		} else if resp.StatusCode >= 400 {
			registrationAttempts.WithLabelValues("4xx", eventType).Inc()
		} else {
			registrationAttempts.WithLabelValues("unexpected", eventType).Inc()
		}
	}
}
