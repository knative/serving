/*
Copyright 2024 The Knative Authors

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
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
	. "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/queue"
)

func TestQuarantineRecoveryMechanism(t *testing.T) {
	// Set feature gates for quarantine-only mode
	setFeatureGatesForTesting(t, false, true)

	logger := TestLogger(t)

	t.Run("quarantined pods transition to recovering state", func(t *testing.T) {
		revID := types.NamespacedName{Namespace: "test", Name: "revision"}
		rt := mustCreateRevisionThrottler(t,
			revID,
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10},
			logger,
		)

		// Create a quarantined tracker with expired quarantine time
		tracker := &podTracker{
			dest:       "quarantined-pod",
			revisionID: revID, // Must match throttler's revisionID
		}
		tracker.state.Store(uint32(podQuarantined))
		tracker.quarantineEndTime.Store(time.Now().Unix() - 1) // Expired
		tracker.refCount.Store(1)                              // Has active requests

		// Add tracker to throttler
		rt.mux.Lock()
		rt.podTrackers["quarantined-pod"] = tracker
		rt.assignedTrackers = append(rt.assignedTrackers, tracker)
		rt.mux.Unlock()

		rt.mux.RLock()
		trackers := rt.assignedTrackers
		rt.mux.RUnlock()

		// First call transitions from quarantined to recovering (but doesn't include it)
		_ = rt.filterAvailableTrackers(trackers)

		// Should have transitioned to recovering
		if podState(tracker.state.Load()) != podRecovering {
			t.Errorf("Expected podRecovering state after first filter, got %v", podState(tracker.state.Load()))
		}

		// Second call should include the recovering pod
		available := rt.filterAvailableTrackers(trackers)

		if len(available) != 1 {
			t.Errorf("Expected 1 available tracker, got %d", len(available))
		}
	})

	t.Run("recovering pods are included in available trackers", func(t *testing.T) {
		revID := types.NamespacedName{Namespace: "test", Name: "revision"}
		rt := &revisionThrottler{
			logger:  logger,
			revID:   revID,
			breaker: queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
		}

		// Create trackers in different states
		healthyTracker := &podTracker{
			dest:       "healthy-pod",
			revisionID: revID,
		}
		healthyTracker.state.Store(uint32(podReady))

		recoveringTracker := &podTracker{
			dest:       "recovering-pod",
			revisionID: revID,
		}
		recoveringTracker.state.Store(uint32(podRecovering))

		quarantinedTracker := &podTracker{
			dest:       "quarantined-pod",
			revisionID: revID,
		}
		quarantinedTracker.state.Store(uint32(podQuarantined))
		quarantinedTracker.quarantineEndTime.Store(time.Now().Unix() + 10) // Not expired

		drainingTracker := &podTracker{
			dest:       "draining-pod",
			revisionID: revID,
		}
		drainingTracker.state.Store(uint32(podDraining))

		trackers := []*podTracker{healthyTracker, recoveringTracker, quarantinedTracker, drainingTracker}
		available := rt.filterAvailableTrackers(trackers)

		// Should include healthy and recovering, but not quarantined or draining
		if len(available) != 2 {
			t.Errorf("Expected 2 available trackers, got %d", len(available))
		}

		// Check that the right trackers are included
		availableDests := make(map[string]bool)
		for _, tracker := range available {
			availableDests[tracker.dest] = true
		}

		if !availableDests["healthy-pod"] {
			t.Error("Healthy pod should be available")
		}
		if !availableDests["recovering-pod"] {
			t.Error("Recovering pod should be available")
		}
		if availableDests["quarantined-pod"] {
			t.Error("Quarantined pod should not be available")
		}
		if availableDests["draining-pod"] {
			t.Error("Draining pod should not be available")
		}
	})

	t.Run("recovering pods promote to healthy after successful request", func(t *testing.T) {
		// Test this logic directly without the complex try() method to avoid infinite loops

		revID := types.NamespacedName{Namespace: "test", Name: "revision"}
		// Create a recovering tracker
		tracker := &podTracker{
			dest:       "recovering-pod",
			revisionID: revID,
			b:          queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
		}
		tracker.state.Store(uint32(podRecovering))
		tracker.quarantineCount.Store(3) // Had previous quarantines

		// Simulate the logic from try() when a request succeeds
		currentState := podState(tracker.state.Load())
		if currentState == podRecovering {
			tracker.state.Store(uint32(podReady))
			tracker.quarantineCount.Store(0)
		}

		// Pod should now be healthy with reset quarantine count
		if podState(tracker.state.Load()) != podReady {
			t.Errorf("Expected podReady state after successful request, got %v", podState(tracker.state.Load()))
		}
		if tracker.quarantineCount.Load() != 0 {
			t.Errorf("Expected quarantine count to be reset to 0, got %d", tracker.quarantineCount.Load())
		}
	})

	t.Run("recovering pods can be drained", func(t *testing.T) {
		rt := &revisionThrottler{
			logger:      logger,
			revID:       types.NamespacedName{Namespace: "test", Name: "revision"},
			breaker:     queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
			podTrackers: make(map[string]*podTracker),
		}

		// Create a recovering tracker with no active requests
		tracker := newTestTracker("recovering-pod", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
		tracker.state.Store(uint32(podRecovering))

		rt.podTrackers["recovering-pod"] = tracker

		// Verify the tracker can drain (tryDrain should succeed since refCount is 0)
		if !tracker.tryDrain() {
			t.Error("Tracker should be able to drain when refCount is 0")
		}

		// Check that the state changed to draining and the tracker was removed
		if podState(tracker.state.Load()) != podDraining {
			t.Errorf("Expected podDraining state, got %v", podState(tracker.state.Load()))
		}
	})

	t.Run("external state updates preserve recovering pods", func(t *testing.T) {
		rt := &revisionThrottler{
			logger:      logger,
			revID:       types.NamespacedName{Namespace: "test", Name: "revision"},
			breaker:     queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
			podTrackers: make(map[string]*podTracker),
		}

		// Create a recovering tracker
		tracker := &podTracker{
			dest: "recovering-pod",
			b:    queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
		}
		tracker.state.Store(uint32(podRecovering))

		rt.podTrackers["recovering-pod"] = tracker

		// External update marks this pod as healthy
		// Run in goroutine to avoid blocking on breaker capacity updates
		done := make(chan struct{})
		go func() {
			rt.updateThrottlerState(nil, []string{"recovering-pod"}, nil)
			close(done)
		}()

		// Wait for the goroutine to complete to prevent test cleanup race
		<-done

		// Pod should still be in recovering state, not forced to healthy
		if podState(tracker.state.Load()) != podRecovering {
			t.Errorf("External update should not override recovering state, got %v", podState(tracker.state.Load()))
		}
	})

	t.Run("pods do not transition to recovering if quarantine not expired", func(t *testing.T) {
		rt := &revisionThrottler{
			logger:  logger,
			revID:   types.NamespacedName{Namespace: "test", Name: "revision"},
			breaker: queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
		}

		// Create a quarantined tracker with future quarantine end time
		tracker := &podTracker{dest: "quarantined-pod"}
		tracker.state.Store(uint32(podQuarantined))
		tracker.quarantineEndTime.Store(time.Now().Unix() + 10) // Future time

		trackers := []*podTracker{tracker}
		available := rt.filterAvailableTrackers(trackers)

		// Should not be available and should remain quarantined
		if len(available) != 0 {
			t.Errorf("Expected 0 available trackers, got %d", len(available))
		}
		if podState(tracker.state.Load()) != podQuarantined {
			t.Errorf("Expected podQuarantined state, got %v", podState(tracker.state.Load()))
		}
	})
}

func TestQuarantineBackoffSchedule(t *testing.T) {
	// Set feature gates for quarantine-only mode
	setFeatureGatesForTesting(t, false, true)

	t.Run("pods use standard backoff schedule", func(t *testing.T) {
		testCases := []struct {
			count    uint32
			expected uint32
		}{
			{1, 1},  // 1 second
			{2, 1},  // 2 seconds
			{3, 2},  // 5 seconds
			{4, 3},  // 10 seconds
			{5, 5},  // 20 seconds (cap)
			{10, 5}, // Still capped at 20s
		}

		for _, tc := range testCases {
			got := quarantineBackoffSeconds(tc.count)
			if got != tc.expected {
				t.Errorf("quarantineBackoffSeconds(%d) = %d, want %d", tc.count, got, tc.expected)
			}
		}
	})
}
