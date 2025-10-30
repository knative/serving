package net

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/types"
	netheader "knative.dev/networking/pkg/http/header"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/activator/handler"
	"knative.dev/serving/pkg/queue"
)

const (
	// Pod ready check timeout used to verify queue-proxy health
	podReadyCheckTimeout = 5000 * time.Millisecond
)

// Quarantine system functions (only used when enableQuarantine=true)

// decrementQuarantineGauge decrements the quarantine gauge if pod is in quarantined state.
// Returns the current quarantine count for this pod (0 if not quarantined or quarantine disabled).
func decrementQuarantineGauge(ctx context.Context, p *podTracker) uint64 {
	_, quarantineEnabled := getFeatureGates()

	if !quarantineEnabled || p == nil {
		return 0
	}

	state := podState(p.state.Load())
	if state == podQuarantined {
		// Decrement gauge for quarantined pod
		handler.RecordPodQuarantineChange(ctx, -1)
		handler.RecordPodQuarantineExit(ctx)
		return uint64(p.quarantineCount.Load())
	}

	return 0
}

// quarantineBackoffSeconds returns backoff seconds for a given consecutive quarantine count.
// Implements a single backoff schedule: 1s, 1s, 2s, 3s, 5s+ (caps at 5s for subsequent attempts).
// This schedule balances quick retry for transient failures with backpressure for persistent issues.
func quarantineBackoffSeconds(count uint32) uint32 {
	switch count {
	case 1:
		return 1
	case 2:
		return 1
	case 3:
		return 2
	case 4:
		return 3
	default:
		return 5
	}
}

// podReadyCheckFunc holds the function used for health checking pods
var podReadyCheckFunc atomic.Value

// podReadyCheckClient is reused across all health checks to avoid allocating a new client per call
var podReadyCheckClient = &http.Client{
	Timeout: podReadyCheckTimeout,
	Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext: (&net.Dialer{
			Timeout: podReadyCheckTimeout,
		}).DialContext,
	},
}

func init() {
	podReadyCheckFunc.Store(podReadyCheck)
}

// podReadyCheck performs HTTP health check against queue-proxy
func podReadyCheck(dest string, expectedRevision types.NamespacedName) error {
	ctx, cancel := context.WithTimeout(context.Background(), podReadyCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+dest+"/", nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "kube-probe/activator")
	req.Header.Set(netheader.ProbeKey, queue.Name)

	resp, err := podReadyCheckClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("non 200 status code, %v", resp.StatusCode)
	}

	respRevName := resp.Header.Get("X-Knative-Revision-Name")
	respRevNamespace := resp.Header.Get("X-Knative-Revision-Namespace")

	// Backwards compatibility: If headers are not present (old queue-proxy), skip validation
	if respRevName == "" && respRevNamespace == "" {
		return nil
	}

	// If headers ARE present, validate they match expected revision
	if respRevName != expectedRevision.Name || respRevNamespace != expectedRevision.Namespace {
		if logger := logging.FromContext(context.Background()); logger != nil {
			logger.Errorw("Health check response from wrong revision - IP reuse detected!",
				"dest", dest,
				"expected-revision", expectedRevision.String(),
				"response-revision", types.NamespacedName{Name: respRevName, Namespace: respRevNamespace}.String())
		}
		return fmt.Errorf("health check response from wrong revision - IP reuse detected! %q %q != %q",
			dest,
			expectedRevision.String(),
			types.NamespacedName{Name: respRevName, Namespace: respRevNamespace}.String())
	}

	return nil
}

// tcpPingCheck invokes the pod ready check function
func tcpPingCheck(dest string, expectedRevision types.NamespacedName) error {
	featureGateMutex.RLock()
	quarantineEnabled := enableQuarantine
	featureGateMutex.RUnlock()

	if !quarantineEnabled {
		return nil // Skip health checks when quarantine is disabled
	}
	fn := podReadyCheckFunc.Load().(func(string, types.NamespacedName) error)
	return fn(dest, expectedRevision)
}

// performHealthCheckAndQuarantine runs health check on a routable pod and quarantines it if check fails.
// Returns:
// - wasQuarantined: true if pod was quarantined (caller should re-enqueue request)
// - isStaleTracker: true if IP reuse was detected (caller should enqueue opRemovePod)
// - healthCheckMs: health check duration in milliseconds
// Only performs health check on podReady and podRecovering states (not podNotReady).
func performHealthCheckAndQuarantine(ctx context.Context, tracker *podTracker, xRequestId string) (wasQuarantined bool, isStaleTracker bool, healthCheckMs float64) {
	_, quarantineEnabled := getFeatureGates()
	if !quarantineEnabled {
		return false, false, 0
	}

	currentState := podState(tracker.state.Load())

	// Skip health checks for podNotReady (not routable)
	if currentState != podReady && currentState != podRecovering {
		return false, false, 0
	}

	// Track health check duration
	healthCheckStart := time.Now()
	healthCheckError := tcpPingCheck(tracker.dest, tracker.revisionID)
	healthCheckMs = float64(time.Since(healthCheckStart).Milliseconds())

	if healthCheckError == nil {
		return false, false, healthCheckMs
	}

	// Check if error indicates IP reuse (stale tracker)
	// IP reuse means this pod IP now belongs to a different revision
	if strings.Contains(healthCheckError.Error(), "IP reuse detected") {
		// This tracker belongs to a different revision - signal for immediate removal
		// Don't quarantine, just return flag so caller can enqueue opRemovePod
		return false, true, healthCheckMs
	}

	// Health check failed (not IP reuse) - try to quarantine
	// Use CAS loop to handle concurrent state changes
	wasQuarantined = false
	for !wasQuarantined {
		prevState := podState(tracker.state.Load())
		if prevState == podQuarantined {
			// Already quarantined by another goroutine
			break
		}
		// Only quarantine from routable states
		if prevState != podReady && prevState != podRecovering {
			break
		}
		if tracker.state.CompareAndSwap(uint32(prevState), uint32(podQuarantined)) {
			wasQuarantined = true
			// Increment consecutive quarantine count
			count := tracker.quarantineCount.Add(1)
			// Determine backoff duration
			backoff := quarantineBackoffSeconds(count)
			tracker.quarantineEndTime.Store(time.Now().Unix() + int64(backoff))
			// Record metrics
			handler.RecordPodQuarantineChange(ctx, 1)
			handler.RecordPodQuarantineEntry(ctx)
			handler.RecordTCPPingFailureEvent(ctx)
		}
	}

	return wasQuarantined, false, healthCheckMs
}
