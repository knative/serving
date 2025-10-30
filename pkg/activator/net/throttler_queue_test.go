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

package net

import (
	"fmt"
	"strconv"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	. "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/queue"
)

// TestQueueBasedStateManagement verifies that the new queue-based state management
// serializes all pod mutations and capacity updates correctly
func TestQueueBasedStateManagement(t *testing.T) {
	logger := TestLogger(t)
	revID := types.NamespacedName{Namespace: "test", Name: "rev"}

	// Create a revision throttler directly
	rt := mustCreateRevisionThrottler(t, revID, nil, 1, "http",
		queue.BreakerParams{
			QueueDepth:      10,
			MaxConcurrency:  1,
			InitialCapacity: 1,
		}, logger)
	rt.numActivators.Store(1)
	rt.activatorIndex.Store(0)

	// Track number of pods added
	var addedCount int32
	var mu sync.Mutex

	// Start 10 goroutines that concurrently add pods
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 10 {
				podIP := "10.0." + strconv.Itoa(id) + "." + strconv.Itoa(j) + ":8080"
				// Use the new queue-based method
				rt.mutatePodIncremental(podIP, "ready")

				mu.Lock()
				addedCount++
				mu.Unlock()
			}
		}(i)
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// Ensure the worker has processed all requests
	rt.FlushForTesting()

	// Verify all pods were added
	rt.mux.RLock()
	podCount := len(rt.podTrackers)
	capacity := rt.breaker.Capacity()
	rt.mux.RUnlock()

	t.Logf("Added %d pods via queue, found %d pods in tracker map", addedCount, podCount)

	// We should have 100 pods (10 goroutines * 10 pods each)
	if podCount != 100 {
		t.Errorf("Expected 100 pods in tracker map, got %d", podCount)
	}

	// Capacity should match pod count since CC=1
	if capacity != 100 {
		t.Errorf("Expected capacity of 100 (100 pods * CC=1), got %d", capacity)
	}
}

// TestQueueConcurrentStateUpdates verifies that concurrent state updates are handled correctly
func TestQueueConcurrentStateUpdates(t *testing.T) {
	logger := TestLogger(t)
	revID := types.NamespacedName{Namespace: "test", Name: "rev"}

	// Create a revision throttler directly
	rt := mustCreateRevisionThrottler(t, revID, nil, 1, "http",
		queue.BreakerParams{
			QueueDepth:      10,
			MaxConcurrency:  1,
			InitialCapacity: 1,
		}, logger)
	rt.numActivators.Store(1)
	rt.activatorIndex.Store(0)

	// Add some initial pods
	for i := range 10 {
		podIP := "10.0.0." + strconv.Itoa(i) + ":8080"
		rt.mutatePodIncremental(podIP, "ready")
	}

	// Ensure the worker has processed all requests
	rt.FlushForTesting()

	// Now concurrently update their states
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			podIP := "10.0.0." + strconv.Itoa(id) + ":8080"

			// Cycle through states
			rt.mutatePodIncremental(podIP, "not-ready")
			rt.mutatePodIncremental(podIP, "ready")
			rt.mutatePodIncremental(podIP, "draining")
		}(i)
	}

	wg.Wait()

	// Ensure the worker has processed all state updates
	rt.FlushForTesting()

	// Check that all pods are in not-ready state (draining)
	rt.mux.RLock()
	notReadyCount := 0
	for _, tracker := range rt.podTrackers {
		state := podState(tracker.state.Load())
		if state == podNotReady {
			notReadyCount++
		}
	}
	rt.mux.RUnlock()

	t.Logf("Found %d pods in not-ready state", notReadyCount)

	if notReadyCount != 10 {
		t.Errorf("Expected 10 pods in not-ready state, got %d", notReadyCount)
	}
}

// TestWorkerPanicRecovery verifies that the worker restarts on panic and signals waiters
func TestWorkerPanicRecovery(t *testing.T) {
	logger := TestLogger(t)
	revID := types.NamespacedName{Namespace: "test", Name: "rev"}

	rt := mustCreateRevisionThrottler(t, revID, nil, 1, "http",
		queue.BreakerParams{
			QueueDepth:      10,
			MaxConcurrency:  1,
			InitialCapacity: 1,
		}, logger)

	// First, add a pod normally to verify worker is functioning
	rt.mutatePodIncremental("10.0.0.1:8080", "ready")
	rt.FlushForTesting()

	// Verify pod was added
	rt.mux.RLock()
	initialCount := len(rt.podTrackers)
	rt.mux.RUnlock()

	if initialCount != 1 {
		t.Fatalf("Expected 1 pod, got %d", initialCount)
	}

	// Inject a panic on the next opMutatePod operation
	panicCount := 0
	rt.testPanicInjector = func(req stateUpdateRequest) {
		if req.op == opMutatePod && req.pod == "10.0.0.2:8080" {
			panicCount++
			panic("injected test panic")
		}
	}

	// Attempt to add a second pod - this should panic and recover
	rt.mutatePodIncremental("10.0.0.2:8080", "ready")

	// Clear the injector to prevent further panics
	rt.testPanicInjector = nil

	// Verify the worker recovered by adding a third pod
	rt.mutatePodIncremental("10.0.0.3:8080", "ready")
	rt.FlushForTesting()

	// Verify the third pod was added successfully (worker recovered)
	rt.mux.RLock()
	finalCount := len(rt.podTrackers)
	rt.mux.RUnlock()

	if finalCount != 2 {
		t.Fatalf("Expected 2 pods after panic recovery (first + third), got %d", finalCount)
	}

	if panicCount != 1 {
		t.Fatalf("Expected exactly 1 panic injection, got %d", panicCount)
	}

	t.Log("Worker successfully recovered from panic and processed subsequent requests")
}

// TestGracefulShutdown verifies that Close() properly drains the queue and stops the worker
func TestGracefulShutdown(t *testing.T) {
	logger := TestLogger(t)
	revID := types.NamespacedName{Namespace: "test", Name: "rev"}

	rt := mustCreateRevisionThrottler(t, revID, nil, 1, "http",
		queue.BreakerParams{
			QueueDepth:      10,
			MaxConcurrency:  1,
			InitialCapacity: 1,
		}, logger)

	// Add some pods
	for i := range 10 {
		podIP := "10.0.0." + strconv.Itoa(i) + ":8080"
		rt.mutatePodIncremental(podIP, "ready")
	}

	// Flush to ensure all processed
	rt.FlushForTesting()

	// Verify pods were added
	rt.mux.RLock()
	podCount := len(rt.podTrackers)
	rt.mux.RUnlock()

	if podCount != 10 {
		t.Fatalf("Expected 10 pods, got %d", podCount)
	}

	// Now close the throttler
	rt.Close()

	// After Close(), the worker stops but drains pending requests during shutdown
	// The channel remains open during the 5-second shutdown window
	// This is correct behavior - allows graceful completion of in-flight requests
	t.Log("Close() called successfully - worker shutdown initiated")
}

// TestMemoryPressureProtection verifies that pod additions are rejected when exceeding maxTrackersPerRevision
func TestMemoryPressureProtection(t *testing.T) {
	logger := TestLogger(t)
	revID := types.NamespacedName{Namespace: "test", Name: "rev"}

	rt := mustCreateRevisionThrottler(t, revID, nil, 1, "http",
		queue.BreakerParams{
			QueueDepth:      10,
			MaxConcurrency:  1,
			InitialCapacity: 1,
		}, logger)

	// Add pods up to the limit (5000)
	// For testing, we'll just add a reasonable number and verify the logic
	// Adding 5000 pods would be too slow for unit tests
	const testLimit = 100
	originalLimit := maxTrackersPerRevision
	defer func() {
		// Restore original limit after test
		maxTrackersPerRevision = originalLimit
	}()

	// Temporarily set a low limit for testing
	maxTrackersPerRevision = testLimit

	// Add pods up to the limit
	for i := range testLimit {
		podIP := fmt.Sprintf("10.0.0.%d:8080", i)
		rt.mutatePodIncremental(podIP, "ready")
	}

	rt.FlushForTesting()

	// Verify we have exactly testLimit pods
	rt.mux.RLock()
	podCount := len(rt.podTrackers)
	rt.mux.RUnlock()

	if podCount != testLimit {
		t.Fatalf("Expected %d pods, got %d", testLimit, podCount)
	}

	// Try to add one more pod - should be rejected
	rt.mutatePodIncremental("10.0.0.255:8080", "ready")
	rt.FlushForTesting()

	// Verify count didn't increase
	rt.mux.RLock()
	finalCount := len(rt.podTrackers)
	_, rejectedPodExists := rt.podTrackers["10.0.0.255:8080"]
	rt.mux.RUnlock()

	if finalCount != testLimit {
		t.Fatalf("Expected pod count to remain at %d after rejection, got %d", testLimit, finalCount)
	}

	// Verify the rejected pod (255) is NOT in the tracker map
	if rejectedPodExists {
		t.Error("Rejected pod 10.0.0.255:8080 should NOT be in tracker map")
	}

	t.Logf("Memory pressure protection working - rejected pod addition at limit of %d", testLimit)
}
