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
	"context"
	"os"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/serving/pkg/queue"

	. "knative.dev/pkg/logging/testing"
)

// TestMain sets feature gates for QP authority mode (QP events authoritative)
func TestMain(m *testing.M) {
	// QP authority enabled, quarantine disabled
	setFeatureGatesForTesting(true, false)

	// Mock health check to always return true for tests (avoid real HTTP requests)
	podReadyCheckFunc.Store(func(dest string, expectedRevision types.NamespacedName) bool {
		return true // Always healthy in tests
	})

	code := m.Run()
	resetFeatureGatesForTesting()
	os.Exit(code)
}

// TestQPAuthorityOverridesInformer tests that QP events override K8s informer
func TestQPAuthorityOverridesInformer(t *testing.T) {
	logger := TestLogger(t)

	t.Run("QP not-ready overrides K8s healthy (fresh QP data)", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// 1. Create pod via QP startup
		rt.addPodIncremental(podIP, "startup", logger)

		// 2. QP says ready
		rt.addPodIncremental(podIP, "ready", logger)

		// Verify pod is ready
		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()
		if podState(tracker.state.Load()) != podReady {
			t.Fatal("Pod should be ready after QP ready event")
		}

		// 3. QP says not-ready
		rt.addPodIncremental(podIP, "not-ready", logger)

		// Verify pod is pending
		if podState(tracker.state.Load()) != podNotReady {
			t.Error("Pod should be pending after QP not-ready event")
		}

		// 4. K8s informer says healthy (within 30s of QP not-ready)
		// This should be IGNORED because QP recently said not-ready
		rt.updateThrottlerState(1, nil, []string{podIP}, nil, nil)

		// Verify pod is STILL pending (QP authority wins)
		if podState(tracker.state.Load()) != podNotReady {
			t.Error("Pod should stay pending - QP authority should override fresh informer")
		}
	})

	t.Run("QP ready overrides K8s draining (fresh QP data)", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// 1. Create pod via QP and promote to ready
		rt.addPodIncremental(podIP, "startup", logger)
		rt.addPodIncremental(podIP, "ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// Verify pod is ready
		if podState(tracker.state.Load()) != podReady {
			t.Fatal("Pod should be ready")
		}

		// 2. K8s informer says draining (within 30s of QP ready)
		// This should be IGNORED because QP recently confirmed ready
		rt.updateThrottlerState(1, nil, nil, []string{podIP}, nil)

		// Verify pod is STILL ready (QP authority wins)
		if podState(tracker.state.Load()) != podReady {
			t.Error("Pod should stay ready - QP authority should override stale informer drain signal")
		}
	})

	t.Run("Stale QP data allows K8s to promote", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// 1. Create pod via QP startup
		rt.addPodIncremental(podIP, "startup", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// 2. Manually set QP data to be old (>60s ago) to simulate stale QP
		tracker.lastQPUpdate.Store(time.Now().Unix() - 70) // 70s ago
		tracker.lastQPState.Store("not-ready")             // Last said not-ready

		// Verify pod is pending
		if podState(tracker.state.Load()) != podNotReady {
			t.Fatal("Pod should be pending initially")
		}

		// 3. K8s informer says healthy (but QP data is stale >60s)
		// This should SUCCEED because QP data is old (QP likely dead)
		rt.updateThrottlerState(1, nil, []string{podIP}, nil, nil)

		// Verify pod was promoted to ready (informer wins with stale QP)
		if podState(tracker.state.Load()) != podReady {
			t.Error("Pod should be promoted to ready - stale QP data should not block informer")
		}
	})

	t.Run("No QP data - K8s informer is authoritative", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// 1. Create pod via K8s informer (no QP data)
		tracker := newPodTracker(podIP, rt.revID, queue.NewBreaker(queue.BreakerParams{
			QueueDepth:      10,
			MaxConcurrency:  1,
			InitialCapacity: 1,
		}))

		rt.mux.Lock()
		rt.podTrackers[podIP] = tracker
		rt.mux.Unlock()

		// Verify QP never spoke
		if tracker.lastQPUpdate.Load() != 0 {
			t.Fatal("QP update time should be 0 (never heard from QP)")
		}

		// Verify pod is pending
		if podState(tracker.state.Load()) != podNotReady {
			t.Fatal("Pod should be pending initially")
		}

		// 2. K8s informer says healthy (no QP objection)
		// This should SUCCEED because we never heard from QP
		rt.updateThrottlerState(1, nil, []string{podIP}, nil, nil)

		// Verify pod was promoted to ready
		if podState(tracker.state.Load()) != podReady {
			t.Error("Pod should be promoted to ready - K8s is authoritative when QP never spoke")
		}
	})
}

// TestPodStateTransitionPreservesBreaker tests that state transitions don't break active requests
func TestPodStateTransitionPreservesBreaker(t *testing.T) {
	logger := TestLogger(t)

	t.Run("ready to not-ready preserves refCount and breaker", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 10, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Create pod and promote to ready
		rt.addPodIncremental(podIP, "startup", logger)
		rt.addPodIncremental(podIP, "ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// Simulate active requests
		ctx := context.Background()
		release1, ok := tracker.Reserve(ctx)
		if !ok {
			t.Fatal("Should be able to reserve on ready pod")
		}
		release2, ok := tracker.Reserve(ctx)
		if !ok {
			t.Fatal("Should be able to reserve on ready pod")
		}

		initialRefCount := tracker.getRefCount()
		initialCapacity := tracker.Capacity()

		// Demote to pending via QP not-ready
		rt.addPodIncremental(podIP, "not-ready", logger)

		// Verify state changed to pending
		if podState(tracker.state.Load()) != podNotReady {
			t.Error("Pod should be pending after not-ready event")
		}

		// Verify refCount preserved
		if tracker.getRefCount() != initialRefCount {
			t.Errorf("RefCount should be preserved: got %d, want %d", tracker.getRefCount(), initialRefCount)
		}

		// Verify breaker capacity preserved
		if tracker.Capacity() != initialCapacity {
			t.Errorf("Breaker capacity should be preserved: got %d, want %d", tracker.Capacity(), initialCapacity)
		}

		// Release active requests
		release1()
		release2()

		// Verify refCount decreased
		if tracker.getRefCount() != 0 {
			t.Errorf("RefCount should be 0 after releases: got %d", tracker.getRefCount())
		}

		// Verify we CANNOT reserve new requests on pending pod
		_, ok = tracker.Reserve(ctx)
		if ok {
			t.Error("Should not be able to reserve on pending pod")
		}
	})

	t.Run("draining pod completes active requests", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 10, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Create ready pod with active requests
		rt.addPodIncremental(podIP, "startup", logger)
		rt.addPodIncremental(podIP, "ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		ctx := context.Background()
		release1, _ := tracker.Reserve(ctx)
		release2, _ := tracker.Reserve(ctx)
		release3, _ := tracker.Reserve(ctx)

		if tracker.getRefCount() != 3 {
			t.Fatalf("Should have 3 active requests, got %d", tracker.getRefCount())
		}

		// Mark pod draining
		rt.addPodIncremental(podIP, "draining", logger)

		// Verify state is draining
		if podState(tracker.state.Load()) != podDraining {
			t.Error("Pod should be draining")
		}

		// Verify pod still in tracker (active requests)
		rt.mux.RLock()
		_, exists := rt.podTrackers[podIP]
		rt.mux.RUnlock()
		if !exists {
			t.Error("Draining pod with active requests should not be removed")
		}

		// Release requests one by one
		release1()
		if tracker.getRefCount() != 2 {
			t.Errorf("RefCount should be 2, got %d", tracker.getRefCount())
		}

		release2()
		if tracker.getRefCount() != 1 {
			t.Errorf("RefCount should be 1, got %d", tracker.getRefCount())
		}

		// Pod should still exist with 1 request
		rt.mux.RLock()
		_, exists = rt.podTrackers[podIP]
		rt.mux.RUnlock()
		if !exists {
			t.Error("Draining pod should exist until last request completes")
		}

		// Cannot reserve new requests on draining pod
		_, ok := tracker.Reserve(ctx)
		if ok {
			t.Error("Should not be able to reserve on draining pod")
		}

		// Last release - pod should be removed
		release3()

		// Verify pod was removed (handled by next informer update in real code)
		if tracker.getRefCount() != 0 {
			t.Errorf("RefCount should be 0, got %d", tracker.getRefCount())
		}
	})
}

// TestQPEventSequences tests various QP event sequences
func TestQPEventSequences(t *testing.T) {
	logger := TestLogger(t)

	t.Run("startup → ready → not-ready → ready cycle", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Startup
		rt.addPodIncremental(podIP, "startup", logger)
		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()
		if podState(tracker.state.Load()) != podNotReady {
			t.Error("After startup, pod should be pending")
		}

		// Ready
		rt.addPodIncremental(podIP, "ready", logger)
		if podState(tracker.state.Load()) != podReady {
			t.Error("After ready, pod should be ready")
		}

		// Not-ready
		rt.addPodIncremental(podIP, "not-ready", logger)
		if podState(tracker.state.Load()) != podNotReady {
			t.Error("After not-ready, pod should be pending")
		}

		// Ready again
		rt.addPodIncremental(podIP, "ready", logger)
		if podState(tracker.state.Load()) != podReady {
			t.Error("After second ready, pod should be ready again")
		}
	})

	t.Run("draining from ready state", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Get to ready state
		rt.addPodIncremental(podIP, "startup", logger)
		rt.addPodIncremental(podIP, "ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// Drain
		rt.addPodIncremental(podIP, "draining", logger)
		if podState(tracker.state.Load()) != podDraining {
			t.Error("After draining event, pod should be draining")
		}

		// Cannot reserve on draining pod
		_, ok := tracker.Reserve(context.Background())
		if ok {
			t.Error("Should not be able to reserve on draining pod")
		}
	})

	t.Run("QP events update freshness tracking", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		beforeTime := time.Now().Unix()

		// Send startup event
		rt.addPodIncremental(podIP, "startup", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		afterTime := time.Now().Unix()

		// Verify QP tracking updated
		qpUpdate := tracker.lastQPUpdate.Load()
		if qpUpdate < beforeTime || qpUpdate > afterTime {
			t.Errorf("QP update time %d should be between %d and %d", qpUpdate, beforeTime, afterTime)
		}

		qpState := tracker.lastQPState.Load().(string)
		if qpState != "startup" {
			t.Errorf("QP last state should be 'startup', got %q", qpState)
		}

		// Send ready event
		beforeTime = time.Now().Unix()
		rt.addPodIncremental(podIP, "ready", logger)
		afterTime = time.Now().Unix()

		// Verify tracking updated again
		qpUpdate = tracker.lastQPUpdate.Load()
		if qpUpdate < beforeTime || qpUpdate > afterTime {
			t.Errorf("QP update time should be updated to recent time")
		}

		qpState = tracker.lastQPState.Load().(string)
		if qpState != "ready" {
			t.Errorf("QP last state should be 'ready', got %q", qpState)
		}
	})
}

// TestInformerWithQPCoexistence tests K8s informer and QP working together
func TestInformerWithQPCoexistence(t *testing.T) {
	logger := TestLogger(t)

	t.Run("informer creates not-ready, QP promotes to ready", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// K8s informer creates pod
		rt.handleUpdate(revisionDestsUpdate{
			Rev:   rt.revID,
			Dests: sets.New(podIP),
		})

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// Should be ready (informer promoted it via healthyDests)
		if podState(tracker.state.Load()) != podReady {
			t.Error("Informer should have promoted pod to ready")
		}

		// QP can send ready event (should be noop/duplicate)
		rt.addPodIncremental(podIP, "ready", logger)

		// Should still be ready
		if podState(tracker.state.Load()) != podReady {
			t.Error("Pod should remain ready")
		}
	})

	t.Run("QP creates pending, informer promotes to ready", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// QP sends startup (creates pending pod)
		rt.addPodIncremental(podIP, "startup", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		if podState(tracker.state.Load()) != podNotReady {
			t.Fatal("QP startup should create pending pod")
		}

		// K8s informer says healthy
		rt.updateThrottlerState(1, nil, []string{podIP}, nil, nil)

		// Should be promoted to ready
		if podState(tracker.state.Load()) != podReady {
			t.Error("Informer should have promoted pending pod to ready")
		}
	})

	t.Run("informer removes pod, QP re-creates it", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Create via informer
		rt.handleUpdate(revisionDestsUpdate{
			Rev:   rt.revID,
			Dests: sets.New(podIP),
		})

		// Informer removes pod
		rt.handleUpdate(revisionDestsUpdate{
			Rev:   rt.revID,
			Dests: sets.New[string](), // Empty
		})

		// Verify pod removed
		rt.mux.RLock()
		_, exists := rt.podTrackers[podIP]
		rt.mux.RUnlock()
		if exists {
			t.Error("Pod should be removed after informer update")
		}

		// QP re-creates pod (e.g., pod restart, informer hasn't caught up)
		rt.addPodIncremental(podIP, "startup", logger)

		// Verify pod exists again
		rt.mux.RLock()
		tracker, exists := rt.podTrackers[podIP]
		rt.mux.RUnlock()
		if !exists {
			t.Fatal("QP should be able to re-create pod")
		}

		if podState(tracker.state.Load()) != podNotReady {
			t.Error("Re-created pod should be pending")
		}
	})
}

// TestPodNotReadyNonViable tests that podNotReady pods don't receive traffic
func TestPodNotReadyNonViable(t *testing.T) {
	logger := TestLogger(t)

	t.Run("not-ready pods excluded from filterAvailableTrackers", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		// Create mix of pending and ready pods
		pod1 := newPodTracker("10.0.0.1:8080", rt.revID, nil)
		pod1.state.Store(uint32(podNotReady))

		pod2 := newPodTracker("10.0.0.2:8080", rt.revID, nil)
		pod2.state.Store(uint32(podReady))

		pod3 := newPodTracker("10.0.0.3:8080", rt.revID, nil)
		pod3.state.Store(uint32(podNotReady))

		trackers := []*podTracker{pod1, pod2, pod3}

		// Filter
		available := rt.filterAvailableTrackers(trackers)

		// Only pod2 (ready) should be available
		if len(available) != 1 {
			t.Errorf("Expected 1 available pod, got %d", len(available))
		}

		if len(available) > 0 && available[0].dest != pod2.dest {
			t.Errorf("Available pod should be %s, got %s", pod2.dest, available[0].dest)
		}
	})

	t.Run("Reserve() rejects not-ready pods", func(t *testing.T) {
		tracker := newPodTracker("10.0.0.1:8080",
			types.NamespacedName{Namespace: "test", Name: "rev"},
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))

		tracker.state.Store(uint32(podNotReady))

		ctx := context.Background()
		_, ok := tracker.Reserve(ctx)

		if ok {
			t.Error("Reserve() should reject not-ready pods")
		}

		// Verify refCount is 0 (didn't increment)
		if tracker.getRefCount() != 0 {
			t.Errorf("RefCount should be 0 after rejected reserve, got %d", tracker.getRefCount())
		}
	})

	t.Run("not-ready pods excluded from routing but counted in capacity", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		// Create 3 pending pods, 2 ready pods
		for i := range 3 {
			podIP := "10.0.0." + string(rune('1'+i)) + ":8080"
			rt.addPodIncremental(podIP, "startup", logger) // Creates pending
		}

		for i := 3; i < 5; i++ {
			podIP := "10.0.0." + string(rune('1'+i)) + ":8080"
			rt.addPodIncremental(podIP, "startup", logger)
			rt.addPodIncremental(podIP, "ready", logger) // Promotes to ready
		}

		// Capacity is based on ALL trackers (potential capacity)
		// Filtering happens at routing time
		capacity := rt.breaker.Capacity()
		if capacity != 5 {
			t.Errorf("Capacity should be 5 (all pods), got %d", capacity)
		}

		// But only 2 pods should be routable
		rt.mux.RLock()
		available := rt.filterAvailableTrackers(rt.assignedTrackers)
		rt.mux.RUnlock()

		if len(available) != 2 {
			t.Errorf("Only 2 pods should be available for routing, got %d", len(available))
		}
	})
}

// TestDrainingWithActiveRequests tests draining behavior with in-flight requests
func TestDrainingWithActiveRequests(t *testing.T) {
	logger := TestLogger(t)

	t.Run("draining pod not removed until refCount zero", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 10, "http", // CC=10 to allow multiple concurrent requests
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Create ready pod
		rt.addPodIncremental(podIP, "startup", logger)
		rt.addPodIncremental(podIP, "ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// Update tracker capacity to allow 5 requests
		tracker.UpdateConcurrency(5)

		// Simulate 5 active requests
		ctx := context.Background()
		releases := make([]func(), 5)
		for i := range 5 {
			rel, ok := tracker.Reserve(ctx)
			if !ok {
				t.Fatalf("Failed to reserve request %d", i)
			}
			releases[i] = rel
		}

		if tracker.getRefCount() != 5 {
			t.Fatalf("Should have 5 active requests, got %d", tracker.getRefCount())
		}

		// Drain pod
		rt.addPodIncremental(podIP, "draining", logger)

		// Pod should still exist (has active requests)
		rt.mux.RLock()
		_, exists := rt.podTrackers[podIP]
		rt.mux.RUnlock()
		if !exists {
			t.Error("Draining pod should not be removed while requests active")
		}

		// Simulate K8s informer trying to remove draining pod (refCount > 0)
		rt.updateThrottlerState(0, nil, nil, []string{podIP}, nil)

		// Pod should STILL exist (refCount > 0)
		rt.mux.RLock()
		_, exists = rt.podTrackers[podIP]
		rt.mux.RUnlock()
		if !exists {
			t.Error("Draining pod should not be removed until refCount == 0")
		}

		// Release all requests
		for _, rel := range releases {
			rel()
		}

		// Verify refCount is 0
		if tracker.getRefCount() != 0 {
			t.Errorf("RefCount should be 0 after all releases, got %d", tracker.getRefCount())
		}

		// Now informer can remove it
		rt.updateThrottlerState(0, nil, nil, []string{podIP}, nil)

		// Pod should be removed
		rt.mux.RLock()
		_, exists = rt.podTrackers[podIP]
		rt.mux.RUnlock()
		if exists {
			t.Error("Pod should be removed after refCount reaches 0")
		}
	})
}

// TestQPvsInformerTimingScenarios tests timing-based authority
func TestQPvsInformerTimingScenarios(t *testing.T) {
	logger := TestLogger(t)

	t.Run("fresh QP data blocks informer promotion", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// QP creates pod and says not-ready
		rt.addPodIncremental(podIP, "startup", logger)
		rt.addPodIncremental(podIP, "not-ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// Verify QP just spoke
		qpAge := time.Now().Unix() - tracker.lastQPUpdate.Load()
		if qpAge > 5 {
			t.Errorf("QP should have spoken recently, age: %d seconds", qpAge)
		}

		// K8s informer says healthy (QP data is fresh <30s)
		rt.updateThrottlerState(1, nil, []string{podIP}, nil, nil)

		// Pod should STAY pending (QP authority)
		if podState(tracker.state.Load()) != podNotReady {
			t.Error("Fresh QP not-ready should block informer promotion")
		}
	})

	t.Run("stale QP data allows informer to drain", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 10, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// QP creates ready pod
		rt.addPodIncremental(podIP, "startup", logger)
		rt.addPodIncremental(podIP, "ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// Give it capacity and add an active request to prevent immediate removal
		tracker.UpdateConcurrency(5)
		ctx := context.Background()
		release, _ := tracker.Reserve(ctx)
		defer release()

		// Simulate QP being silent for >60s (stale)
		tracker.lastQPUpdate.Store(time.Now().Unix() - 70)
		tracker.lastQPState.Store("ready")

		// K8s informer says draining (QP data is stale >60s)
		rt.updateThrottlerState(0, nil, nil, []string{podIP}, nil)

		// Pod should be draining (informer wins with stale QP)
		// Pod not removed because refCount > 0
		if podState(tracker.state.Load()) != podDraining {
			t.Error("Stale QP data should allow informer to drain pod")
		}

		// Verify pod still exists (active request)
		rt.mux.RLock()
		_, exists := rt.podTrackers[podIP]
		rt.mux.RUnlock()
		if !exists {
			t.Error("Draining pod with active request should not be removed")
		}
	})
}

// TestStateMachineValidation tests state machine validation and edge case handling
func TestStateMachineValidation(t *testing.T) {
	logger := TestLogger(t)

	t.Run("draining on not-ready pod - crash before ready", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Pod starts up but crashes before becoming ready
		rt.addPodIncremental(podIP, "startup", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		if podState(tracker.state.Load()) != podNotReady {
			t.Fatal("Pod should be pending after startup")
		}

		// Pod sends draining event (crash during startup)
		rt.addPodIncremental(podIP, "draining", logger)

		// Should transition pending → draining
		if podState(tracker.state.Load()) != podDraining {
			t.Error("Pod should transition from pending to draining on crash")
		}

		// Verify draining timestamp was set
		if tracker.drainingStartTime.Load() == 0 {
			t.Error("Draining start time should be set")
		}
	})

	t.Run("stale ready event on draining pod - ignored", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Get to draining state
		rt.addPodIncremental(podIP, "startup", logger)
		rt.addPodIncremental(podIP, "ready", logger)
		rt.addPodIncremental(podIP, "draining", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		if podState(tracker.state.Load()) != podDraining {
			t.Fatal("Pod should be draining")
		}

		// Stale ready event arrives (out of order)
		rt.addPodIncremental(podIP, "ready", logger)

		// Should STAY draining (ready event ignored)
		if podState(tracker.state.Load()) != podDraining {
			t.Error("Pod should stay draining - stale ready event should be ignored")
		}
	})

	t.Run("stale not-ready event on draining pod - ignored", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Get to draining state
		rt.addPodIncremental(podIP, "startup", logger)
		rt.addPodIncremental(podIP, "ready", logger)
		rt.addPodIncremental(podIP, "draining", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		if podState(tracker.state.Load()) != podDraining {
			t.Fatal("Pod should be draining")
		}

		// Stale not-ready event arrives
		rt.addPodIncremental(podIP, "not-ready", logger)

		// Should STAY draining (not-ready event ignored)
		if podState(tracker.state.Load()) != podDraining {
			t.Error("Pod should stay draining - stale not-ready event should be ignored")
		}
	})

	t.Run("not-ready on pending pod - probe flapping", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Pod starts but probe keeps failing (flapping)
		rt.addPodIncremental(podIP, "startup", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// Send not-ready on pending pod (probe flapping before first success)
		rt.addPodIncremental(podIP, "not-ready", logger)

		// Should STAY pending (not-ready on pending is noop)
		if podState(tracker.state.Load()) != podNotReady {
			t.Error("Pod should stay pending - not-ready on pending is ignored")
		}
	})

	t.Run("ready after not-ready - pod recovery", func(t *testing.T) {
		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Normal flow: startup → ready → not-ready → ready
		rt.addPodIncremental(podIP, "startup", logger)
		rt.addPodIncremental(podIP, "ready", logger)

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// Pod becomes unhealthy
		rt.addPodIncremental(podIP, "not-ready", logger)
		if podState(tracker.state.Load()) != podNotReady {
			t.Error("Pod should be pending after not-ready")
		}

		// Pod recovers
		rt.addPodIncremental(podIP, "ready", logger)
		if podState(tracker.state.Load()) != podReady {
			t.Error("Pod should be ready again after recovery")
		}
	})
}
