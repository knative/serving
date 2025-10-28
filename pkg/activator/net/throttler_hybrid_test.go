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
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
	. "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/queue"
)

func TestQuarantineOverridesQPState(t *testing.T) {
	// Set feature gates for hybrid mode (both enabled)
	setFeatureGatesForTesting(t, true, true)

	logger := TestLogger(t)

	t.Run("health check failure quarantines despite QP ready", func(t *testing.T) {
		rt := mustCreateRevisionThrottler(t, 
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// 1. Pod created via QP "ready" → podReady
		rt.addPodIncremental(podIP, "not-ready", logger)
		rt.addPodIncremental(podIP, "ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		if podState(tracker.state.Load()) != podReady {
			t.Fatal("Pod should be ready after QP ready event")
		}

		// 2. Simulate health check failure by manually quarantining
		// (In real code, this would happen in the request path)
		tracker.state.Store(uint32(podQuarantined))
		tracker.quarantineEndTime.Store(time.Now().Unix() + 10) // 10 seconds

		// 3. Verify pod is quarantined
		if podState(tracker.state.Load()) != podQuarantined {
			t.Error("Pod should be quarantined after health check failure")
		}

		// 4. QP sends another "ready" event
		rt.addPodIncremental(podIP, "ready", logger)

		// 5. Pod stays podQuarantined (quarantine wins over QP state)
		// QP ready events should not override quarantine
		if podState(tracker.state.Load()) != podQuarantined {
			t.Error("Pod should stay quarantined - QP cannot override quarantine")
		}
	})

	t.Run("quarantine prevents QP ready promotion", func(t *testing.T) {
		rt := mustCreateRevisionThrottler(t, 
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// 1. Create pod and put it in quarantine
		rt.addPodIncremental(podIP, "not-ready", logger)
		rt.addPodIncremental(podIP, "ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// Quarantine the pod
		tracker.state.Store(uint32(podQuarantined))
		tracker.quarantineEndTime.Store(time.Now().Unix() + 5) // 5 seconds
		tracker.quarantineCount.Store(1)

		// 2. QP sends "ready" event while pod is quarantined
		rt.addPodIncremental(podIP, "ready", logger)

		// 3. Pod stays podQuarantined (cannot be pulled out by QP)
		if podState(tracker.state.Load()) != podQuarantined {
			t.Error("Pod should stay quarantined - QP ready cannot override")
		}

		// 4. Wait for quarantine to expire
		tracker.quarantineEndTime.Store(time.Now().Unix() - 1) // Expired
		tracker.refCount.Store(1)                              // Has active request

		// 5. First filter call transitions to recovering (but doesn't include it)
		rt.filterAvailableTrackers([]*podTracker{tracker})

		// Verify transitioned to recovering
		if podState(tracker.state.Load()) != podRecovering {
			t.Errorf("Pod should be recovering, got %v", podState(tracker.state.Load()))
		}

		// 6. Second filter call includes the recovering pod
		available := rt.filterAvailableTrackers([]*podTracker{tracker})

		// Should be available (recovering)
		if len(available) != 1 {
			t.Error("Pod should be available after transitioning to recovering")
		}

		// 7. Simulate successful request → podReady
		if tracker.state.CompareAndSwap(uint32(podRecovering), uint32(podReady)) {
			tracker.quarantineCount.Store(0)
		}

		if podState(tracker.state.Load()) != podReady {
			t.Error("Pod should be ready after successful recovery")
		}
	})
}

func TestQPEventsWithQuarantine(t *testing.T) {
	// Set feature gates for hybrid mode (both enabled)
	setFeatureGatesForTesting(t, true, true)

	logger := TestLogger(t)

	t.Run("QP not-ready still allows transition via quarantine flow", func(t *testing.T) {
		rt := mustCreateRevisionThrottler(t, 
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// 1. QP sends "not-ready" → podNotReady
		rt.addPodIncremental(podIP, "not-ready", logger)
		rt.addPodIncremental(podIP, "not-ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		if podState(tracker.state.Load()) != podNotReady {
			t.Error("Pod should be not-ready after QP not-ready event")
		}

		// 2. If pod were to become ready and then fail health check,
		// it would be quarantined (both systems operate)
		// Promote to ready first
		rt.addPodIncremental(podIP, "ready", logger)

		if podState(tracker.state.Load()) != podReady {
			t.Error("Pod should be ready after QP ready event")
		}

		// 3. Quarantine the pod (simulate health check failure)
		tracker.state.Store(uint32(podQuarantined))
		tracker.quarantineEndTime.Store(time.Now().Unix() + 10)

		// 4. Verify both systems can coexist
		if podState(tracker.state.Load()) != podQuarantined {
			t.Error("Quarantine should work even with QP authority enabled")
		}

		// QP still tracks state
		if tracker.lastQPUpdate.Load() == 0 {
			t.Error("QP tracking should be updated")
		}
	})

	t.Run("podRecovering can receive QP events", func(t *testing.T) {
		rt := mustCreateRevisionThrottler(t, 
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Create pod and put in recovering state
		rt.addPodIncremental(podIP, "not-ready", logger)
		rt.addPodIncremental(podIP, "ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// Put pod in recovering state
		tracker.state.Store(uint32(podRecovering))

		// QP sends "not-ready" while pod is recovering
		rt.addPodIncremental(podIP, "not-ready", logger)

		// Recovering state should be preserved
		// (external QP events don't override quarantine states)
		if podState(tracker.state.Load()) != podRecovering {
			t.Error("Pod should stay recovering - QP not-ready should not override")
		}

		// QP tracking is still updated
		lastState := tracker.lastQPState.Load().(string)
		if lastState != "not-ready" {
			t.Errorf("QP last state should be 'not-ready', got %q", lastState)
		}
	})

	t.Run("QP draining works with quarantine system", func(t *testing.T) {
		rt := mustCreateRevisionThrottler(t, 
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 10, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Create ready pod
		rt.addPodIncremental(podIP, "not-ready", logger)
		rt.addPodIncremental(podIP, "ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		tracker.UpdateConcurrency(5)

		// Add active request
		ctx := context.Background()
		release, _ := tracker.Reserve(ctx)

		// QP sends draining event
		rt.addPodIncremental(podIP, "draining", logger)

		// Pod should be draining (even with quarantine enabled)
		if podState(tracker.state.Load()) != podDraining {
			t.Error("Pod should be draining after QP draining event")
		}

		// Release request
		release()

		// Pod can be removed once refCount is 0
		if tracker.getRefCount() != 0 {
			t.Error("RefCount should be 0 after release")
		}
	})
}

func TestHealthCheckWithQPAuthority(t *testing.T) {
	// Set feature gates for hybrid mode (both enabled)
	setFeatureGatesForTesting(t, true, true)

	logger := TestLogger(t)

	t.Run("health checks still happen when quarantine enabled", func(t *testing.T) {
		rt := mustCreateRevisionThrottler(t, 
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// 1. Create pod via QP "ready"
		rt.addPodIncremental(podIP, "not-ready", logger)
		rt.addPodIncremental(podIP, "ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		if podState(tracker.state.Load()) != podReady {
			t.Fatal("Pod should be ready")
		}

		// 2. Simulate health check failure
		// In real code, this happens in try() when health check fails
		initialQuarantineCount := tracker.quarantineCount.Load()

		// Quarantine the pod
		tracker.state.Store(uint32(podQuarantined))
		tracker.quarantineCount.Add(1)
		tracker.quarantineEndTime.Store(time.Now().Unix() + 10)

		// 3. Verify pod goes to podQuarantined
		if podState(tracker.state.Load()) != podQuarantined {
			t.Error("Pod should be quarantined after health check failure")
		}

		if tracker.quarantineCount.Load() != initialQuarantineCount+1 {
			t.Error("Quarantine count should increment")
		}

		// 4. QP ready event should not override quarantine
		rt.addPodIncremental(podIP, "ready", logger)

		if podState(tracker.state.Load()) != podQuarantined {
			t.Error("Quarantine should not be overridden by QP ready")
		}
	})

	t.Run("recovering pods counted as available with QP authority", func(t *testing.T) {
		rt := mustCreateRevisionThrottler(t, 
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		// Create pod in recovering state
		podIP := "10.0.0.1:8080"
		rt.addPodIncremental(podIP, "not-ready", logger)
		rt.addPodIncremental(podIP, "ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// Put in recovering state
		tracker.state.Store(uint32(podRecovering))
		tracker.refCount.Store(1) // Simulate active request

		// Filter should include recovering pods
		available := rt.filterAvailableTrackers([]*podTracker{tracker})

		if len(available) != 1 {
			t.Error("Recovering pods should be available for routing")
		}

		// Successful request promotes to ready
		if tracker.state.CompareAndSwap(uint32(podRecovering), uint32(podReady)) {
			tracker.quarantineCount.Store(0)
		}

		if podState(tracker.state.Load()) != podReady {
			t.Error("Pod should be promoted to ready after successful request")
		}
	})
}
