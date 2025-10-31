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

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	. "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/queue"
)

// TestDefaultModeVerification tests the legacy default mode behavior
// This tests the mode where QP authority is OFF, quarantine is ON
// (the previous production default before QP authority was enabled)

func TestDefaultModeVerification(t *testing.T) {
	// Set feature gates for legacy default mode: QP authority OFF, quarantine ON
	setFeatureGatesForTesting(t, false, true)

	logger := TestLogger(t)

	t.Run("verify default behavior is quarantine mode", func(t *testing.T) {
		rt := mustCreateRevisionThrottler(t,
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Create pod via informer
		rt.handleUpdate(revisionDestsUpdate{
			Rev:   rt.revID,
			Dests: sets.New(podIP),
		})

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// In default mode (QP=false, quarantine=true):
		// - Informer creates pod as podNotReady
		// - Immediately promoted to podReady (no QP authority)
		if podState(tracker.state.Load()) != podReady {
			t.Errorf("Default mode should promote pod to ready immediately, got %v", podState(tracker.state.Load()))
		}

		// Verify pod is routable
		ctx := context.Background()
		release, ok := tracker.Reserve(ctx)
		if !ok {
			t.Error("Pod should be routable in default mode")
		}
		if release != nil {
			release()
		}
	})
}

func TestInformerCreatesReadyPods(t *testing.T) {
	// Set feature gates for legacy default mode: QP authority OFF, quarantine ON
	setFeatureGatesForTesting(t, false, true)

	logger := TestLogger(t)

	t.Run("informer creates and promotes pods immediately", func(t *testing.T) {
		rt := mustCreateRevisionThrottler(t,
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Call handleUpdate with pod
		rt.handleUpdate(revisionDestsUpdate{
			Rev:   rt.revID,
			Dests: sets.New(podIP),
		})

		rt.mux.RLock()
		tracker, exists := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		// Verify pod was created
		if !exists {
			t.Fatal("Pod should be created by informer")
		}

		// Verify pod was promoted to podReady immediately
		// (in default mode, no QP authority, so informer creates as ready)
		if podState(tracker.state.Load()) != podReady {
			t.Errorf("Pod should be podReady after informer creation, got %v", podState(tracker.state.Load()))
		}

		// Verify pod is routable
		ctx := context.Background()
		release, ok := tracker.Reserve(ctx)
		if !ok {
			t.Error("Pod should be routable")
		}
		if release != nil {
			release()
		}
	})

	t.Run("multiple pods created by informer", func(t *testing.T) {
		rt := mustCreateRevisionThrottler(t,
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		pod1 := "10.0.0.1:8080"
		pod2 := "10.0.0.2:8080"
		pod3 := "10.0.0.3:8080"

		// Create multiple pods
		rt.handleUpdate(revisionDestsUpdate{
			Rev:   rt.revID,
			Dests: sets.New(pod1, pod2, pod3),
		})

		rt.mux.RLock()
		defer rt.mux.RUnlock()

		// Verify all pods were created and are ready
		for _, podIP := range []string{pod1, pod2, pod3} {
			tracker, exists := rt.podTrackers[podIP]
			if !exists {
				t.Errorf("Pod %s should exist", podIP)
				continue
			}

			if podState(tracker.state.Load()) != podReady {
				t.Errorf("Pod %s should be ready, got %v", podIP, podState(tracker.state.Load()))
			}
		}
	})
}

func TestHealthCheckFlowDefault(t *testing.T) {
	// Set feature gates for legacy default mode: QP authority OFF, quarantine ON
	setFeatureGatesForTesting(t, false, true)

	logger := TestLogger(t)

	t.Run("QP events received but do not trigger state changes", func(t *testing.T) {
		rt := mustCreateRevisionThrottler(t,
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Create pod via informer (should be ready)
		rt.handleUpdate(revisionDestsUpdate{
			Rev:   rt.revID,
			Dests: sets.New(podIP),
		})

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		initialState := podState(tracker.state.Load())
		if initialState != podReady {
			t.Fatalf("Pod should start ready, got %v", initialState)
		}

		// Send QP "not-ready" event (should be ignored in default mode)
		rt.mutatePodIncremental(podIP, "not-ready")

		// In default mode, QP events don't trigger state changes
		// Pod should stay ready
		if podState(tracker.state.Load()) != podReady {
			t.Error("In default mode, QP events should not change pod state")
		}

		// Verify QP tracking data is updated (events are received)
		if tracker.lastQPUpdate.Load() == 0 {
			t.Error("QP tracking should be updated even in default mode")
		}

		lastState := tracker.lastQPState.Load()
		if lastState == nil {
			t.Error("QP last state should be recorded")
		}
	})

	t.Run("pods transition to not-ready when removed by informer", func(t *testing.T) {
		rt := mustCreateRevisionThrottler(t,
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 10, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)

		podIP := "10.0.0.1:8080"

		// Create ready pod
		rt.handleUpdate(revisionDestsUpdate{
			Rev:   rt.revID,
			Dests: sets.New(podIP),
		})

		rt.mux.RLock()
		tracker := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		if tracker == nil {
			t.Fatal("Pod should exist")
		}

		// Verify pod is ready
		if podState(tracker.state.Load()) != podReady {
			t.Fatalf("Pod should be ready, got %v", podState(tracker.state.Load()))
		}

		// Remove pod via informer (empty dest set)
		rt.handleUpdate(revisionDestsUpdate{
			Rev:   rt.revID,
			Dests: sets.New[string](),
		})

		// Pod should be removed immediately since refCount=0
		rt.mux.RLock()
		_, exists := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		if exists {
			t.Error("Pod should be removed from tracker map (no active requests)")
		}
	})

	// Regression test for bug where "draining" events created new pods as ready
	// when QP authority was disabled
	t.Run("draining event for new pod is rejected (QP authority OFF)", func(t *testing.T) {
		rt := mustCreateRevisionThrottler(t,
			types.NamespacedName{Namespace: "test", Name: "revision"},
			nil, 1, "http",
			queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 10},
			logger,
		)
		rt.numActivators.Store(1)
		rt.activatorIndex.Store(0)

		podIP := "10.0.0.1:8080"

		// Pod sends draining event (QP authority OFF, so shouldn't affect state)
		// But previously this incorrectly created pod as ready!
		rt.mutatePodIncremental(podIP, "draining")

		// Verify pod was NOT added to the tracker
		rt.mux.RLock()
		tracker, exists := rt.podTrackers[podIP]
		rt.mux.RUnlock()

		if exists {
			t.Errorf("Pod should NOT be added when draining event is first event (even with QP authority OFF), but got state=%v", podState(tracker.state.Load()))
		}
	})
}
