package net

import (
	"testing"
	"time"
	"sync"
	"strconv"

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
	rt := newRevisionThrottler(revID, nil, 1, "http",
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
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				podIP := "10.0." + strconv.Itoa(id) + "." + strconv.Itoa(j) + ":8080"
				// Use the new queue-based method
				rt.addPodIncremental(podIP, "ready", logger)

				mu.Lock()
				addedCount++
				mu.Unlock()

				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// Give the worker goroutine time to process all requests
	time.Sleep(100 * time.Millisecond)

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
	rt := newRevisionThrottler(revID, nil, 1, "http",
		queue.BreakerParams{
			QueueDepth:      10,
			MaxConcurrency:  1,
			InitialCapacity: 1,
		}, logger)
	rt.numActivators.Store(1)
	rt.activatorIndex.Store(0)

	// Add some initial pods
	for i := 0; i < 10; i++ {
		podIP := "10.0.0." + strconv.Itoa(i) + ":8080"
		rt.addPodIncremental(podIP, "ready", logger)
	}

	// Give worker time to process
	time.Sleep(50 * time.Millisecond)

	// Now concurrently update their states
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			podIP := "10.0.0." + strconv.Itoa(id) + ":8080"

			// Cycle through states
			rt.addPodIncremental(podIP, "not-ready", logger)
			time.Sleep(5 * time.Millisecond)
			rt.addPodIncremental(podIP, "ready", logger)
			time.Sleep(5 * time.Millisecond)
			rt.addPodIncremental(podIP, "draining", logger)
		}(i)
	}

	wg.Wait()

	// Give worker time to process all state updates
	time.Sleep(100 * time.Millisecond)

	// Check that all pods are in draining state
	rt.mux.RLock()
	drainingCount := 0
	for _, tracker := range rt.podTrackers {
		state := podState(tracker.state.Load())
		if state == podDraining {
			drainingCount++
		}
	}
	rt.mux.RUnlock()

	t.Logf("Found %d pods in draining state", drainingCount)

	if drainingCount != 10 {
		t.Errorf("Expected 10 pods in draining state, got %d", drainingCount)
	}
}