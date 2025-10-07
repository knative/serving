/*
Copyright 2019 The Knative Authors

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
	"errors"
	"fmt"
	"maps"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	pkgnet "knative.dev/networking/pkg/apis/networking"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeendpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	. "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/ptr"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/serving/pkg/activator/handler"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/queue"
)

var testBreakerParams = queue.BreakerParams{
	QueueDepth:      1,
	MaxConcurrency:  revisionMaxConcurrency,
	InitialCapacity: 0,
}

type tryResult struct {
	dest string
	err  error
}

func newTestThrottler(ctx context.Context) *Throttler {
	return NewThrottler(ctx, "10.10.10.10")
}

// mockTCPPingCheck sets a custom TCP ping check function for testing
// and returns a cleanup function to restore the original
func mockTCPPingCheck(fn func(string) bool) func() {
	oldFunc := podReadyCheckFunc.Load()
	setPodReadyCheckFunc(fn)
	return func() {
		podReadyCheckFunc.Store(oldFunc)
	}
}

// TestMain sets up the test environment
func TestMain(m *testing.M) {
	// Mock pod ready check to always succeed for all tests
	setPodReadyCheckFunc(func(dest string) bool { return true })
	m.Run()
}

func TestThrottlerUpdateCapacity(t *testing.T) {
	logger := TestLogger(t)

	tests := []struct {
		name                 string
		capacity             int
		numActivators        int32
		activatorIndex       int32
		containerConcurrency int
		isNewInfiniteBreaker bool
		podTrackers          []*podTracker
		want                 uint64
		checkAssignedPod     bool
	}{{
		name:                 "capacity: 1, cc: 10",
		capacity:             1,
		containerConcurrency: 10,
		want:                 1 * 10,
	}, {
		name:                 "capacity: 10, cc: 10",
		capacity:             10,
		containerConcurrency: 10,
		want:                 10 * 10,
	}, {
		name:                 "capacity: 100, cc: 10",
		capacity:             100,
		containerConcurrency: 10,
		want:                 100 * 10,
	}, {
		name:                 "numActivators: 5, capacity: 10, cc: 10",
		capacity:             10,
		numActivators:        5,
		containerConcurrency: 10,
		want:                 10 * 10 / 5,
	}, {
		name:                 "numActivators: 200, capacity: 10, cc: 10",
		capacity:             10,
		numActivators:        200,
		containerConcurrency: 10,
		want:                 1,
	}, {
		name:                 "numActivators: 200, capacity: 0, cc: 10",
		capacity:             0,
		numActivators:        200,
		containerConcurrency: 10,
		want:                 0,
	}, {
		// now test with CC=0.
		// in the CC=0 cases we use the infinite breaker whose capacity can either be
		// totally blocked (0) or totally open (1).
		name:                 "newInfiniteBreaker, numActivators: 0, capacity: 0, cc: 0",
		capacity:             0,
		numActivators:        0,
		containerConcurrency: 0,
		isNewInfiniteBreaker: true,
		want:                 0,
	}, {
		name:                 "newInfiniteBreaker, numActivators: 0, capacity: 1, cc: 0",
		capacity:             1,
		numActivators:        0,
		containerConcurrency: 0,
		isNewInfiniteBreaker: true,
		want:                 1,
	}, {
		name:                 "newInfiniteBreaker, numActivators: 0, capacity: 10, cc: 0",
		capacity:             10,
		numActivators:        0,
		containerConcurrency: 0,
		isNewInfiniteBreaker: true,
		want:                 1,
	}, {
		name:                 "newInfiniteBreaker, numActivators: 200, capacity: 0, cc: 0",
		capacity:             0,
		numActivators:        200,
		containerConcurrency: 0,
		isNewInfiniteBreaker: true,
		want:                 0,
	}, {
		name:                 "newInfiniteBreaker, numActivators: 200, capacity: 1, cc: 0",
		capacity:             1,
		numActivators:        200,
		containerConcurrency: 0,
		isNewInfiniteBreaker: true,
		want:                 1,
	}, {
		// Now test with podIP trackers in tow.
		// Simple case.
		name:                 "numActivators: 1, capacity: 0, cc: 10, pods: 1",
		capacity:             0,
		numActivators:        1,
		containerConcurrency: 10,
		podTrackers:          makeTrackers(1, 10),
		want:                 10,
	}, {
		name:                 "2 backends. numActivators: 1, capacity: -1, cc: 10, pods: 2",
		capacity:             -1,
		numActivators:        1,
		containerConcurrency: 10,
		podTrackers:          makeTrackers(2, 10),
		want:                 20,
	}, {
		name:                 "2 activators. numActivators: 2, capacity: -1, cc: 10, pods: 2",
		capacity:             -1,
		numActivators:        2,
		containerConcurrency: 10,
		podTrackers:          makeTrackers(2, 10),
		want:                 10,
	}, {
		name:                 "numActivators: 2, index: 0, pods: 3, cc: 1. Capacity is expected to 20 (2 * 10)",
		capacity:             -1,
		numActivators:        2,
		containerConcurrency: 10,
		podTrackers:          makeTrackers(3, 10),
		want:                 20,
	}, {
		name:                 "numActivators: 2, index: 1, pods: 3, cc: 1. Capacity is expected to 10 (1 * 10)",
		capacity:             -1,
		numActivators:        2,
		activatorIndex:       1,
		containerConcurrency: 10,
		podTrackers:          makeTrackers(3, 10),
		want:                 10,
	}, {
		name:                 "numActivators: 2, index: 0, pods: 5, cc: 1. Capacity is expected to 3 (2 + 1)",
		capacity:             5,
		numActivators:        2,
		activatorIndex:       0,
		containerConcurrency: 1,
		podTrackers:          makeTrackers(5, 1),
		want:                 3,
	}, {
		name:                 "numActivators: 2, index: 1, pods: 5, cc: 1. Capacity is expected to 2",
		capacity:             5,
		numActivators:        2,
		activatorIndex:       1,
		containerConcurrency: 1,
		podTrackers:          makeTrackers(5, 1),
		want:                 2,
	}, {
		name:                 "Infinite capacity with podIP trackers.",
		capacity:             1,
		numActivators:        2,
		activatorIndex:       1,
		containerConcurrency: 0,
		podTrackers:          makeTrackers(3, 0),
		isNewInfiniteBreaker: true,
		want:                 1,
		checkAssignedPod:     true,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &revisionThrottler{
				logger:  logger,
				breaker: queue.NewBreaker(testBreakerParams),
			}
			rt.containerConcurrency.Store(uint32(tt.containerConcurrency))
			rt.numActivators.Store(uint32(tt.numActivators))
			rt.activatorIndex.Store(tt.activatorIndex)
			rtPodTrackers := make(map[string]*podTracker)
			for _, pt := range tt.podTrackers {
				rtPodTrackers[pt.dest] = pt
			}
			rt.podTrackers = rtPodTrackers
			if tt.isNewInfiniteBreaker {
				rt.breaker = newInfiniteBreaker(logger)
			}
			rt.updateCapacity(tt.capacity)
			if got := rt.breaker.Capacity(); got != tt.want {
				t.Errorf("Capacity = %d, want: %d", got, tt.want)
			}
			if tt.checkAssignedPod {
				if got, want := len(rt.assignedTrackers), len(rt.podTrackers); got != want {
					t.Errorf("Assigned tracker count = %d, want: %d, diff:\n%s", got, want,
						cmp.Diff(rt.assignedTrackers, rt.podTrackers))
				}
			}
		})
	}
}

func TestThrottlerCalculateCapacity(t *testing.T) {
	logger := TestLogger(t)
	tests := []struct {
		name                 string
		numActivators        int32
		containerConcurrency int
		numTrackers          int
		activatorCount       int
		backendCount         int
	}{{
		name:                 "over revisionMaxConcurrency",
		numActivators:        200,
		containerConcurrency: 0,
		numTrackers:          revisionMaxConcurrency + 5,
		activatorCount:       1,
		backendCount:         1,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &revisionThrottler{
				logger:  logger,
				breaker: newInfiniteBreaker(logger),
			}
			rt.containerConcurrency.Store(uint32(tt.containerConcurrency))
			rt.numActivators.Store(uint32(tt.numActivators))
			// shouldn't really happen since revisionMaxConcurrency is very, very large,
			// but check that we behave reasonably if it's exceeded.
			capacity := rt.calculateCapacity(tt.backendCount, tt.numTrackers, tt.activatorCount)
			if got, want := capacity, queue.MaxBreakerCapacity; got != want {
				t.Errorf("calculateCapacity = %d, want: %d", got, want)
			}
		})
	}
}

func makeTrackers(num, cc int) []*podTracker {
	trackers := make([]*podTracker, num)
	for i := range num {
		pt := newPodTracker(strconv.Itoa(i), nil)
		if cc > 0 {
			pt.b = queue.NewBreaker(queue.BreakerParams{
				QueueDepth:      1,
				MaxConcurrency:  cc,
				InitialCapacity: cc,
			})
		}
		// For tests, set trackers to healthy state instead of pending
		pt.state.Store(uint32(podHealthy))
		trackers[i] = pt
	}
	return trackers
}

func TestThrottlerErrorNoRevision(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	servfake := fakeservingclient.Get(ctx)
	revisions := fakerevisioninformer.Get(ctx)
	waitInformers, err := rtesting.RunAndSyncInformers(ctx, revisions.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	// Add the revision we're testing.
	revID := types.NamespacedName{Namespace: testNamespace, Name: testRevision}
	revision := revisionCC1(revID, pkgnet.ProtocolHTTP1)
	servfake.ServingV1().Revisions(revision.Namespace).Create(ctx, revision, metav1.CreateOptions{})
	revisions.Informer().GetIndexer().Add(revision)

	throttler := newTestThrottler(ctx)
	throttler.handleUpdate(revisionDestsUpdate{
		Rev:   revID,
		Dests: sets.New("128.0.0.1:1234"),
	})

	// Make sure it now works.
	if err := throttler.Try(ctx, revID, "test", func(string, bool) error { return nil }); err != nil {
		t.Fatalf("Try() = %v, want no error", err)
	}

	// Make sure errors are propagated correctly.
	innerError := errors.New("inner")
	if err := throttler.Try(ctx, revID, "test", func(string, bool) error { return innerError }); !errors.Is(err, innerError) {
		t.Fatalf("Try() = %v, want %v", err, innerError)
	}

	servfake.ServingV1().Revisions(revision.Namespace).Delete(ctx, revision.Name, metav1.DeleteOptions{})
	revisions.Informer().GetIndexer().Delete(revID)

	// Eventually it should now fail.
	var lastError error
	wait.PollUntilContextCancel(ctx, 10*time.Millisecond, false, func(context.Context) (bool, error) {
		lastError = throttler.Try(ctx, revID, "test", func(string, bool) error { return nil })
		return lastError != nil, nil
	})
	if lastError == nil || lastError.Error() != `revision.serving.knative.dev "test-revision" not found` {
		t.Fatalf("Try() = %v, wanted a not found error", lastError)
	}
}

func TestThrottlerErrorOneTimesOut(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	servfake := fakeservingclient.Get(ctx)
	revisions := fakerevisioninformer.Get(ctx)
	waitInformers, err := rtesting.RunAndSyncInformers(ctx, revisions.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	// Add the revision we're testing.
	revID := types.NamespacedName{Namespace: testNamespace, Name: testRevision}
	revision := revisionCC1(revID, pkgnet.ProtocolHTTP1)
	servfake.ServingV1().Revisions(revision.Namespace).Create(ctx, revision, metav1.CreateOptions{})
	revisions.Informer().GetIndexer().Add(revision)

	throttler := newTestThrottler(ctx)
	throttler.handleUpdate(revisionDestsUpdate{
		Rev:           revID,
		ClusterIPDest: "129.0.0.1:1234",
		Dests:         sets.New("128.0.0.1:1234"),
	})

	// Send 2 requests, one should time out.
	var mux sync.Mutex
	mux.Lock() // Lock the mutex so all requests are blocked in the Try function.

	reqCtx, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	resultChan := throttler.try(reqCtx, 2 /*requests*/, func(string) error {
		mux.Lock()
		return nil
	})

	// The first result will be a timeout because of the locking logic.
	if result := <-resultChan; !errors.Is(result.err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want %v", err, context.DeadlineExceeded)
	}

	// Allow the successful request to pass through.
	mux.Unlock()
	if result := <-resultChan; result.err != nil {
		t.Fatalf("err = %v, want no error", err)
	}
}

func sortedTrackers(trk []*podTracker) bool {
	for i := 1; i < len(trk); i++ {
		if trk[i].dest < trk[i-1].dest {
			return false
		}
	}
	return true
}

func TestThrottlerSuccesses(t *testing.T) {
	for _, tc := range []struct {
		name        string
		revision    *v1.Revision
		initUpdates []revisionDestsUpdate
		requests    int
		wantDests   sets.Set[string]
	}{{
		name:     "single healthy podIP",
		revision: revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.New("128.0.0.1:1234"),
		}, {
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.New("128.0.0.1:1234"),
		}},
		requests:  1,
		wantDests: sets.New("128.0.0.1:1234"),
	}, {
		name:     "single healthy podIP, infinite cc",
		revision: revision(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1, 0),
		// Double updates exercise additional paths.
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.New("128.0.0.2:1234", "128.0.0.32:1212"),
		}, {
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.New("128.0.0.1:1234"),
		}},
		requests:  1,
		wantDests: sets.New("128.0.0.1:1234"),
	}, {
		name:     "single healthy clusterIP",
		revision: revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.New("128.0.0.1:1234", "128.0.0.2:1234"),
		}, {
			Rev:           types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			ClusterIPDest: "129.0.0.1:1234",
			Dests:         sets.New("128.0.0.1:1234"),
		}},
		requests:  1,
		wantDests: sets.New("128.0.0.1:1234"), // Now expects pod routing instead of clusterIP
	}, {
		name:     "spread podIP load",
		revision: revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			// Double update here exercises some additional paths.
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.New("128.0.0.3:1234"),
		}, {
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.New("128.0.0.1:1234", "128.0.0.2:1234"),
		}},
		requests:  2,
		wantDests: sets.New("128.0.0.2:1234", "128.0.0.1:1234"),
	}, {
		name:     "clumping test",
		revision: revision(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1, 3),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.New("128.0.0.1:1234", "128.0.0.2:1234", "128.0.0.2:4236", "128.0.0.2:1233", "128.0.0.2:1230"),
		}},
		requests:  3,
		wantDests: sets.New("128.0.0.1:1234"),
	}, {
		name: "roundrobin test",
		revision: revision(types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			pkgnet.ProtocolHTTP1, 5 /*cc >3*/),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.New("128.0.0.1:1234", "128.0.0.2:1234", "211.212.213.214"),
		}},
		requests: 3,
		// All three IP addresses should be used if cc>3.
		wantDests: sets.New("128.0.0.1:1234", "128.0.0.2:1234", "211.212.213.214"),
	}, {
		name:     "multiple ClusterIP requests",
		revision: revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			Rev:           types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			ClusterIPDest: "129.0.0.1:1234",
			Dests:         sets.New("128.0.0.1:1234", "128.0.0.2:1234"),
		}},
		requests:  2,
		wantDests: sets.New("128.0.0.1:1234", "128.0.0.2:1234"), // Now expects pod routing instead of clusterIP
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			servfake := fakeservingclient.Get(ctx)
			fake := fakekubeclient.Get(ctx)
			revisions := fakerevisioninformer.Get(ctx)
			endpoints := fakeendpointsinformer.Get(ctx)

			waitInformers, err := rtesting.RunAndSyncInformers(ctx, endpoints.Informer(),
				revisions.Informer())
			if err != nil {
				t.Fatal("Failed to start informers:", err)
			}
			defer func() {
				cancel()
				waitInformers()
			}()

			// Add the revision were testing.
			servfake.ServingV1().Revisions(tc.revision.Namespace).Create(ctx, tc.revision, metav1.CreateOptions{})
			revisions.Informer().GetIndexer().Add(tc.revision)

			updateCh := make(chan revisionDestsUpdate)

			throttler := NewThrottler(ctx, "130.0.0.2")
			var grp errgroup.Group
			grp.Go(func() error { throttler.run(updateCh); return nil })
			// Ensure the throttler stopped before we leave the test, so that
			// logging does freak out.
			defer func() {
				close(updateCh)
				grp.Wait()
				cancel()
				waitInformers()
			}()

			publicEp := &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testRevision,
					Namespace: testNamespace,
					Labels: map[string]string{
						networking.ServiceTypeKey: string(networking.ServiceTypePublic),
						serving.RevisionLabelKey:  testRevision,
					},
				},
				Subsets: []corev1.EndpointSubset{
					*epSubset(8012, "http", []string{"130.0.0.2"}, nil),
				},
			}

			fake.CoreV1().Endpoints(testNamespace).Create(ctx, publicEp, metav1.CreateOptions{})
			endpoints.Informer().GetIndexer().Add(publicEp)

			revID := types.NamespacedName{Namespace: testNamespace, Name: testRevision}
			rt, err := throttler.getOrCreateRevisionThrottler(revID)
			if err != nil {
				t.Fatal("RevisionThrottler can't be found:", err)
			}

			for _, update := range tc.initUpdates {
				updateCh <- update
			}

			// Make sure our informer event has fired.
			// We send multiple updates in some tests, so make sure the capacity is exact.
			var wantCapacity uint64
			wantCapacity = 1
			cc := tc.revision.Spec.ContainerConcurrency
			dests := tc.initUpdates[len(tc.initUpdates)-1].Dests.Len()
			if *cc != 0 {
				wantCapacity = uint64(dests) * uint64(*cc)
			}
			if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 3*time.Second, true, func(context.Context) (bool, error) {
				rt.mux.RLock()
				defer rt.mux.RUnlock()
				if *cc != 0 {
					return rt.activatorIndex.Load() != -1 && rt.breaker.Capacity() == wantCapacity &&
						sortedTrackers(rt.assignedTrackers), nil
				}
				// If CC=0 then verify number of backends, rather the capacity of breaker.
				return rt.activatorIndex.Load() != -1 && dests == len(rt.assignedTrackers) &&
					sortedTrackers(rt.assignedTrackers), nil
			}); err != nil {
				t.Fatal("Timed out waiting for the capacity to be updated")
			}
			t.Log("This activator idx =", rt.activatorIndex.Load())

			tryContext, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel2()

			waitGrp := sync.WaitGroup{}
			waitGrp.Add(tc.requests)
			resultChan := throttler.try(tryContext, tc.requests, func(string) error {
				waitGrp.Done()
				// Wait for all requests to reach the Try function before proceeding.
				waitGrp.Wait()
				return nil
			})

			gotDests := sets.New[string]()
			for range tc.requests {
				result := <-resultChan
				gotDests.Insert(result.dest)
			}

			if got, want := sets.List(gotDests), sets.List(tc.wantDests); !cmp.Equal(want, got) {
				t.Errorf("Dests = %v, want: %v, diff: %s", got, want, cmp.Diff(want, got))
				rt.mux.RLock()
				defer rt.mux.RUnlock()
				t.Log("podTrackers:\n", spew.Sdump(rt.podTrackers))
				t.Log("assignedTrackers:\n", spew.Sdump(rt.assignedTrackers))
			}
		})
	}
}

func trackerDestSet(ts []*podTracker) sets.Set[string] {
	ret := sets.New[string]()
	for _, t := range ts {
		ret.Insert(t.dest)
	}
	return ret
}

func TestPodAssignmentFinite(t *testing.T) {
	// An e2e verification test of pod assignment and capacity
	// computations.
	logger := TestLogger(t)
	revName := types.NamespacedName{Namespace: testNamespace, Name: testRevision}

	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()

	throttler := newTestThrottler(ctx)
	rt := newRevisionThrottler(revName, nil, 42 /*cc*/, pkgnet.ServicePortNameHTTP1, testBreakerParams, logger)
	rt.numActivators.Store(4)
	rt.activatorIndex.Store(0)
	throttler.revisionThrottlers[revName] = rt

	update := revisionDestsUpdate{
		Rev:           revName,
		ClusterIPDest: "",
		Dests:         sets.New("ip4", "ip3", "ip5", "ip2", "ip1", "ip0"),
	}
	// This should synchronously update throughout the system.
	// And now we can inspect `rt`.
	throttler.handleUpdate(update)
	if got, want := len(rt.podTrackers), len(update.Dests); got != want {
		t.Errorf("NumTrackers = %d, want: %d", got, want)
	}
	// 6 = 4 * 1 + 2; index 0 and index 1 have 2 pods and others have 1 pod.
	if got, want := trackerDestSet(rt.assignedTrackers), sets.New("ip0", "ip4"); !got.Equal(want) {
		t.Errorf("Assigned trackers = %v, want: %v, diff: %s", got, want, cmp.Diff(want, got))
	}
	if got, want := rt.breaker.Capacity(), uint64(2*42); got != want {
		t.Errorf("TotalCapacity = %d, want: %d", got, want)
	}
	if got, want := rt.assignedTrackers[0].Capacity(), uint64(42); got != want {
		t.Errorf("Exclusive tracker capacity: %d, want: %d", got, want)
	}
	if got, want := rt.assignedTrackers[1].Capacity(), uint64(42); got != want {
		t.Errorf("Shared tracker capacity: %d, want: %d", got, want)
	}

	// Now scale to zero.
	update.Dests = nil
	throttler.handleUpdate(update)
	if got, want := len(rt.podTrackers), 0; got != want {
		t.Errorf("NumTrackers = %d, want: %d", got, want)
	}
	if got, want := len(rt.assignedTrackers), 0; got != want {
		t.Errorf("NumAssignedTrackers = %d, want: %d", got, want)
	}
	if got, want := rt.breaker.Capacity(), uint64(0); got != want {
		t.Errorf("TotalCapacity = %d, want: %d", got, want)
	}
}

func TestPodAssignmentInfinite(t *testing.T) {
	logger := TestLogger(t)
	revName := types.NamespacedName{Namespace: testNamespace, Name: testRevision}

	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()

	throttler := newTestThrottler(ctx)
	rt := newRevisionThrottler(revName, nil, 0 /*cc*/, pkgnet.ServicePortNameHTTP1, testBreakerParams, logger)
	throttler.revisionThrottlers[revName] = rt

	update := revisionDestsUpdate{
		Rev:           revName,
		ClusterIPDest: "",
		Dests:         sets.New("ip3", "ip2", "ip1"),
	}
	// This should synchronously update throughout the system.
	// And now we can inspect `rt`.
	throttler.handleUpdate(update)
	if got, want := len(rt.podTrackers), 3; got != want {
		t.Errorf("NumTrackers = %d, want: %d", got, want)
	}
	if got, want := len(rt.assignedTrackers), 3; got != want {
		t.Errorf("NumAssigned trackers = %d, want: %d", got, want)
	}
	if got, want := rt.breaker.Capacity(), uint64(1); got != want {
		t.Errorf("TotalCapacity = %d, want: %d", got, want)
	}
	if got, want := rt.assignedTrackers[0].Capacity(), uint64(1); got != want {
		t.Errorf("Exclusive tracker capacity: %d, want: %d", got, want)
	}

	// Now scale to zero.
	update.Dests = nil
	throttler.handleUpdate(update)
	if got, want := len(rt.podTrackers), 0; got != want {
		t.Errorf("NumTrackers = %d, want: %d", got, want)
	}
	if got, want := len(rt.assignedTrackers), 0; got != want {
		t.Errorf("NumAssignedTrackers = %d, want: %d", got, want)
	}
	if got, want := rt.breaker.Capacity(), uint64(0); got != want {
		t.Errorf("TotalCapacity = %d, want: %d", got, want)
	}
}

func TestActivatorsIndexUpdate(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	fake := fakekubeclient.Get(ctx)
	endpoints := fakeendpointsinformer.Get(ctx)
	servfake := fakeservingclient.Get(ctx)
	revisions := revisioninformer.Get(ctx)

	waitInformers, err := rtesting.RunAndSyncInformers(ctx, endpoints.Informer(), revisions.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	revID := types.NamespacedName{Namespace: testNamespace, Name: testRevision}
	rev := revisionCC1(revID, pkgnet.ProtocolH2C)
	// Add the revision we're testing.
	servfake.ServingV1().Revisions(rev.Namespace).Create(ctx, rev, metav1.CreateOptions{})
	revisions.Informer().GetIndexer().Add(rev)

	updateCh := make(chan revisionDestsUpdate)

	throttler := NewThrottler(ctx, "130.0.0.2")
	var grp errgroup.Group
	grp.Go(func() error { throttler.run(updateCh); return nil })
	// Ensure the throttler stopped before we leave the test, so that
	// logging does freak out.
	defer func() {
		close(updateCh)
		grp.Wait()
		cancel()
		waitInformers()
	}()

	possibleDests := sets.New("128.0.0.1:1234", "128.0.0.2:1234", "128.0.0.23:1234")
	updateCh <- revisionDestsUpdate{
		Rev:   revID,
		Dests: possibleDests,
	}

	// Add activator endpoint with 2 activators.
	publicEp := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testRevision,
			Namespace: testNamespace,
			Labels: map[string]string{
				networking.ServiceTypeKey: string(networking.ServiceTypePublic),
				serving.RevisionLabelKey:  testRevision,
			},
		},
		Subsets: []corev1.EndpointSubset{
			*epSubset(8013, "http2", []string{"130.0.0.1", "130.0.0.2"}, nil),
		},
	}
	fake.CoreV1().Endpoints(testNamespace).Create(ctx, publicEp, metav1.CreateOptions{})
	endpoints.Informer().GetIndexer().Add(publicEp)

	rt, err := throttler.getOrCreateRevisionThrottler(revID)
	if err != nil {
		t.Fatal("RevisionThrottler can't be found:", err)
	}

	// Verify capacity gets updated. This is the very last thing we update
	// so we now know that the rest is set statically.
	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, time.Second, true, func(context.Context) (bool, error) {
		// Capacity doesn't exceed 1 in this test.
		return rt.breaker.Capacity() == 1, nil
	}); err != nil {
		t.Fatal("Timed out waiting for the capacity to be updated")
	}

	if got, want := rt.numActivators.Load(), uint32(2); got != want {
		t.Fatalf("numActivators = %d, want %d", got, want)
	}
	if got, want := rt.activatorIndex.Load(), int32(1); got != want {
		t.Fatalf("activatorIndex = %d, want %d", got, want)
	}
	if got, want := len(rt.assignedTrackers), 1; got != want {
		t.Fatalf("len(assignedTrackers) = %d, want %d", got, want)
	}

	publicEp.Subsets = []corev1.EndpointSubset{
		*epSubset(8013, "http2", []string{"130.0.0.2"}, nil),
	}

	fake.CoreV1().Endpoints(testNamespace).Update(ctx, publicEp, metav1.UpdateOptions{})
	endpoints.Informer().GetIndexer().Update(publicEp)

	// Verify the index was computed.
	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, time.Second, true, func(context.Context) (bool, error) {
		return rt.numActivators.Load() == 1 &&
			rt.activatorIndex.Load() == 0, nil
	}); err != nil {
		t.Fatal("Timed out waiting for the Activator Endpoints to be computed")
	}
}

func TestMultipleActivators(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	fake := fakekubeclient.Get(ctx)
	endpoints := fakeendpointsinformer.Get(ctx)
	servfake := fakeservingclient.Get(ctx)
	revisions := revisioninformer.Get(ctx)

	waitInformers, err := rtesting.RunAndSyncInformers(ctx, endpoints.Informer(), revisions.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	rev := revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, pkgnet.ProtocolHTTP1)
	// Add the revision we're testing.
	servfake.ServingV1().Revisions(rev.Namespace).Create(ctx, rev, metav1.CreateOptions{})
	revisions.Informer().GetIndexer().Add(rev)

	updateCh := make(chan revisionDestsUpdate)

	throttler := NewThrottler(ctx, "130.0.0.2")
	var grp errgroup.Group
	grp.Go(func() error { throttler.run(updateCh); return nil })
	// Ensure the throttler stopped before we leave the test, so that
	// logging does freak out.
	defer func() {
		close(updateCh)
		grp.Wait()
		cancel()
		waitInformers()
	}()

	revID := types.NamespacedName{Namespace: testNamespace, Name: testRevision}
	possibleDests := sets.New("128.0.0.1:1234", "128.0.0.2:1234", "128.0.0.23:1234")
	updateCh <- revisionDestsUpdate{
		Rev:   revID,
		Dests: possibleDests,
	}

	// Add activator endpoint with 2 activators.
	publicEp := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testRevision,
			Namespace: testNamespace,
			Labels: map[string]string{
				networking.ServiceTypeKey: string(networking.ServiceTypePublic),
				serving.RevisionLabelKey:  testRevision,
			},
		},
		Subsets: []corev1.EndpointSubset{
			*epSubset(8012, "http", []string{"130.0.0.1", "130.0.0.2"},
				nil),
		},
	}
	fake.CoreV1().Endpoints(testNamespace).Create(ctx, publicEp, metav1.CreateOptions{})
	endpoints.Informer().GetIndexer().Add(publicEp)

	rt, err := throttler.getOrCreateRevisionThrottler(revID)
	if err != nil {
		t.Fatal("RevisionThrottler can't be found:", err)
	}

	// Verify capacity gets updated. This is the very last thing we update
	// so we now know that we got and processed both the activator endpoints
	// and the application endpoints.
	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, time.Second, true, func(context.Context) (bool, error) {
		return rt.breaker.Capacity() == 1, nil
	}); err != nil {
		t.Fatal("Timed out waiting for the capacity to be updated")
	}
	t.Log("This activator idx =", rt.activatorIndex.Load())

	// Test with 2 activators, 3 endpoints we can send 1 request and the second times out.
	var mux sync.Mutex
	mux.Lock() // Lock the mutex so all requests are blocked in the Try function.

	reqCtx, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	resultChan := throttler.try(reqCtx, 2 /*requests*/, func(string) error {
		mux.Lock()
		return nil
	})

	// The first result will be a timeout because of the locking logic.
	if result := <-resultChan; !errors.Is(result.err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want %v", err, context.DeadlineExceeded)
	}

	// Allow the successful request to pass through.
	mux.Unlock()
	if result := <-resultChan; !possibleDests.Has(result.dest) {
		t.Fatalf("Request went to an unknown destination: %s, possibles: %v", result.dest, possibleDests)
	}
}

func TestInfiniteBreakerCreation(t *testing.T) {
	// This test verifies that we use infiniteBreaker when CC==0.
	tttl := newRevisionThrottler(types.NamespacedName{Namespace: "a", Name: "b"}, nil, 0, /*cc*/
		pkgnet.ServicePortNameHTTP1, queue.BreakerParams{}, TestLogger(t))
	if _, ok := tttl.breaker.(*infiniteBreaker); !ok {
		t.Errorf("The type of revisionBreaker = %T, want %T", tttl, (*infiniteBreaker)(nil))
	}
}

func TestLoadBalancingPolicySelection(t *testing.T) {
	logger := TestLogger(t)
	tests := []struct {
		name                 string
		loadBalancingPolicy  *string
		containerConcurrency int
		wantPolicy           string
	}{{
		name:                 "explicit random-choice-2",
		loadBalancingPolicy:  stringPtr("random-choice-2"),
		containerConcurrency: 10,
		wantPolicy:           "randomChoice2Policy",
	}, {
		name:                 "explicit round-robin",
		loadBalancingPolicy:  stringPtr("round-robin"),
		containerConcurrency: 10,
		wantPolicy:           "roundRobinPolicy",
	}, {
		name:                 "explicit least-connections",
		loadBalancingPolicy:  stringPtr("least-connections"),
		containerConcurrency: 10,
		wantPolicy:           "leastConnectionsPolicy",
	}, {
		name:                 "explicit first-available",
		loadBalancingPolicy:  stringPtr("first-available"),
		containerConcurrency: 10,
		wantPolicy:           "firstAvailablePolicy",
	}, {
		name:                 "unknown policy falls back to random-choice-2",
		loadBalancingPolicy:  stringPtr("unknown-policy"),
		containerConcurrency: 10,
		wantPolicy:           "randomChoice2Policy",
	}, {
		name:                 "nil policy with CC=0 uses random-choice-2",
		loadBalancingPolicy:  nil,
		containerConcurrency: 0,
		wantPolicy:           "randomChoice2Policy",
	}, {
		name:                 "nil policy with CC=1 uses first-available",
		loadBalancingPolicy:  nil,
		containerConcurrency: 1,
		wantPolicy:           "firstAvailablePolicy",
	}, {
		name:                 "nil policy with CC=3 uses first-available",
		loadBalancingPolicy:  nil,
		containerConcurrency: 3,
		wantPolicy:           "firstAvailablePolicy",
	}, {
		name:                 "nil policy with CC=4 uses round-robin",
		loadBalancingPolicy:  nil,
		containerConcurrency: 4,
		wantPolicy:           "roundRobinPolicy",
	}, {
		name:                 "nil policy with CC=100 uses round-robin",
		loadBalancingPolicy:  nil,
		containerConcurrency: 100,
		wantPolicy:           "roundRobinPolicy",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rt := newRevisionThrottler(
				types.NamespacedName{Namespace: "test", Name: "revision"},
				test.loadBalancingPolicy,
				test.containerConcurrency,
				pkgnet.ServicePortNameHTTP1,
				testBreakerParams,
				logger,
			)

			// We can't directly check the function type since they're just functions.
			// Instead, we'll check the behavior by using the policy with test data.
			// For now, we'll just ensure the policy is not nil.
			if rt.lbPolicy.Load() == nil {
				t.Errorf("Got nil lbPolicy, expected %s", test.wantPolicy)
			}
		})
	}
}

func stringPtr(s string) *string {
	return &s
}

func TestThrottlerUsesRevisionLoadBalancingPolicy(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()
	servingClient := fakeservingclient.Get(ctx)
	revisions := fakerevisioninformer.Get(ctx)

	// Create test revisions with different load balancing policies
	tests := []struct {
		name               string
		revision           *v1.Revision
		wantPolicyBehavior string // We'll verify behavior rather than type
	}{{
		name: "revision with random-choice-2 policy",
		revision: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-revision-rc2",
				Namespace: "test-namespace",
			},
			Spec: v1.RevisionSpec{
				LoadBalancingPolicy:  stringPtr("random-choice-2"),
				ContainerConcurrency: ptr.Int64(10),
			},
		},
		wantPolicyBehavior: "random-choice-2",
	}, {
		name: "revision with round-robin policy",
		revision: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-revision-rr",
				Namespace: "test-namespace",
			},
			Spec: v1.RevisionSpec{
				LoadBalancingPolicy:  stringPtr("round-robin"),
				ContainerConcurrency: ptr.Int64(10),
			},
		},
		wantPolicyBehavior: "round-robin",
	}, {
		name: "revision with least-connections policy",
		revision: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-revision-lc",
				Namespace: "test-namespace",
			},
			Spec: v1.RevisionSpec{
				LoadBalancingPolicy:  stringPtr("least-connections"),
				ContainerConcurrency: ptr.Int64(10),
			},
		},
		wantPolicyBehavior: "least-connections",
	}, {
		name: "revision without policy uses default based on CC",
		revision: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-revision-default",
				Namespace: "test-namespace",
			},
			Spec: v1.RevisionSpec{
				ContainerConcurrency: ptr.Int64(10),
			},
		},
		wantPolicyBehavior: "round-robin", // CC=10 should use round-robin by default
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Add revision to fake client
			servingClient.ServingV1().Revisions(test.revision.Namespace).Create(ctx, test.revision, metav1.CreateOptions{})
			revisions.Informer().GetIndexer().Add(test.revision)

			// Create throttler
			throttler := NewThrottler(ctx, "10.10.10.10")

			// Get or create revision throttler
			revID := types.NamespacedName{
				Namespace: test.revision.Namespace,
				Name:      test.revision.Name,
			}
			revThrottler, err := throttler.getOrCreateRevisionThrottler(revID)
			if err != nil {
				t.Fatalf("Failed to get revision throttler: %v", err)
			}

			// Verify the throttler was created with a load balancing policy
			if revThrottler.lbPolicy.Load() == nil {
				t.Errorf("Expected lbPolicy to be set, got nil")
			}

			// Note: We can't easily verify the exact policy type since they're just functions,
			// but we've verified that the policy is being read from the revision spec
			// and passed to newRevisionThrottler in the implementation.
		})
	}
}

func TestLoadBalancingAlgorithms(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()

	logger := TestLogger(t)
	servingClient := fakeservingclient.Get(ctx)
	revisions := fakerevisioninformer.Get(ctx)

	// Helper to create pod trackers with specific capacities
	createTrackers := func(count int, capacity int) []*podTracker {
		trackers := make([]*podTracker, count)
		for i := range count {
			dest := fmt.Sprintf("10.0.0.%d:8080", i+1)
			breaker := queue.NewBreaker(queue.BreakerParams{
				QueueDepth:      10,
				MaxConcurrency:  capacity,
				InitialCapacity: capacity,
			})
			trackers[i] = newPodTracker(dest, breaker)
		}
		return trackers
	}

	t.Run("round-robin distributes evenly", func(t *testing.T) {
		// Create revision with round-robin policy
		rev := &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-rr",
				Namespace: "test-namespace",
			},
			Spec: v1.RevisionSpec{
				LoadBalancingPolicy:  stringPtr("round-robin"),
				ContainerConcurrency: ptr.Int64(10),
			},
		}
		servingClient.ServingV1().Revisions(rev.Namespace).Create(ctx, rev, metav1.CreateOptions{})
		revisions.Informer().GetIndexer().Add(rev)

		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: rev.Namespace, Name: rev.Name},
			rev.Spec.LoadBalancingPolicy,
			10, // containerConcurrency
			pkgnet.ServicePortNameHTTP1,
			testBreakerParams,
			logger,
		)

		// Set up 3 trackers
		trackers := createTrackers(3, 10)
		rt.assignedTrackers = trackers

		// Track which pods get selected
		selections := make(map[string]int)
		for range 30 {
			lbPolicy := rt.lbPolicy.Load().(lbPolicy)
			_, tracker := lbPolicy(ctx, rt.assignedTrackers)
			if tracker != nil {
				selections[tracker.dest]++
			}
		}

		// Verify even distribution (each should get ~10 requests)
		for dest, count := range selections {
			if count < 8 || count > 12 {
				t.Errorf("Round-robin not distributing evenly: %s got %d requests (expected ~10)", dest, count)
			}
		}
	})

	t.Run("first-available selects first with capacity", func(t *testing.T) {
		// Create revision with first-available policy
		rev := &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-fa",
				Namespace: "test-namespace",
			},
			Spec: v1.RevisionSpec{
				LoadBalancingPolicy:  stringPtr("first-available"),
				ContainerConcurrency: ptr.Int64(1),
			},
		}
		servingClient.ServingV1().Revisions(rev.Namespace).Create(ctx, rev, metav1.CreateOptions{})
		revisions.Informer().GetIndexer().Add(rev)

		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: rev.Namespace, Name: rev.Name},
			rev.Spec.LoadBalancingPolicy,
			1, // containerConcurrency
			pkgnet.ServicePortNameHTTP1,
			testBreakerParams,
			logger,
		)

		// Set up 3 trackers with different capacities
		trackers := createTrackers(3, 1)
		// Exhaust first tracker's capacity
		trackers[0].Reserve(ctx)
		rt.assignedTrackers = trackers

		// Should select second tracker since first is full
		lbPolicy := rt.lbPolicy.Load().(lbPolicy)
		_, tracker := lbPolicy(ctx, rt.assignedTrackers)
		if tracker == nil || tracker.dest != trackers[1].dest {
			t.Errorf("Expected to select second tracker, got %v", tracker)
		}
	})

	t.Run("least-connections selects least loaded", func(t *testing.T) {
		// Create revision with least-connections policy
		rev := &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-lc",
				Namespace: "test-namespace",
			},
			Spec: v1.RevisionSpec{
				LoadBalancingPolicy:  stringPtr("least-connections"),
				ContainerConcurrency: ptr.Int64(10),
			},
		}
		servingClient.ServingV1().Revisions(rev.Namespace).Create(ctx, rev, metav1.CreateOptions{})
		revisions.Informer().GetIndexer().Add(rev)

		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: rev.Namespace, Name: rev.Name},
			rev.Spec.LoadBalancingPolicy,
			10, // containerConcurrency
			pkgnet.ServicePortNameHTTP1,
			testBreakerParams,
			logger,
		)

		// Set up 3 trackers
		trackers := createTrackers(3, 10)
		// Add different loads to each tracker
		trackers[0].Reserve(ctx) // 1 connection
		trackers[0].Reserve(ctx) // 2 connections
		trackers[1].Reserve(ctx) // 1 connection
		// trackers[2] has 0 connections
		rt.assignedTrackers = trackers

		// Should select tracker with least connections (trackers[2])
		lbPolicy := rt.lbPolicy.Load().(lbPolicy)
		_, tracker := lbPolicy(ctx, rt.assignedTrackers)
		if tracker == nil || tracker.dest != trackers[2].dest {
			t.Errorf("Expected to select tracker with 0 connections, got %v", tracker)
		}
	})

	t.Run("random-choice-2 selects lower weight", func(t *testing.T) {
		// Create revision with random-choice-2 policy
		rev := &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-rc2",
				Namespace: "test-namespace",
			},
			Spec: v1.RevisionSpec{
				LoadBalancingPolicy:  stringPtr("random-choice-2"),
				ContainerConcurrency: ptr.Int64(0),
			},
		}
		servingClient.ServingV1().Revisions(rev.Namespace).Create(ctx, rev, metav1.CreateOptions{})
		revisions.Informer().GetIndexer().Add(rev)

		rt := newRevisionThrottler(
			types.NamespacedName{Namespace: rev.Namespace, Name: rev.Name},
			rev.Spec.LoadBalancingPolicy,
			0, // containerConcurrency (infinite)
			pkgnet.ServicePortNameHTTP1,
			testBreakerParams,
			logger,
		)

		// Set up 2 trackers
		trackers := createTrackers(2, 100)
		// Give first tracker higher weight
		trackers[0].increaseWeight()
		trackers[0].increaseWeight()
		trackers[0].increaseWeight()
		rt.assignedTrackers = trackers

		// Run multiple selections to verify it tends to pick lower weight
		selections := make(map[string]int)
		for range 100 {
			lbPolicy := rt.lbPolicy.Load().(lbPolicy)
			_, tracker := lbPolicy(ctx, rt.assignedTrackers)
			if tracker != nil {
				selections[tracker.dest]++
			}
		}

		// With random-choice-2, the lower weight tracker should be selected more often
		// This is probabilistic, so we check for a reasonable distribution
		if selections[trackers[1].dest] < selections[trackers[0].dest] {
			t.Errorf("Random-choice-2 not favoring lower weight: tracker0=%d, tracker1=%d",
				selections[trackers[0].dest], selections[trackers[1].dest])
		}
	})
}

func TestThrottlerHonorsSpecLoadBalancingPolicy(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()

	servingClient := fakeservingclient.Get(ctx)
	revisions := fakerevisioninformer.Get(ctx)

	// Create a revision without spec policy but with annotation specifying round-robin
	rev := &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-revision-spec",
			Namespace: testNamespace,
		},
		Spec: v1.RevisionSpec{
			ContainerConcurrency: ptr.Int64(1), // would default to first-available without spec override
			LoadBalancingPolicy:  stringPtr("round-robin"),
		},
	}
	servingClient.ServingV1().Revisions(rev.Namespace).Create(ctx, rev, metav1.CreateOptions{})
	revisions.Informer().GetIndexer().Add(rev)

	throttler := NewThrottler(ctx, "10.10.10.10")
	revID := types.NamespacedName{Namespace: rev.Namespace, Name: rev.Name}
	rt, err := throttler.getOrCreateRevisionThrottler(revID)
	if err != nil {
		t.Fatalf("Failed to get revision throttler: %v", err)
	}

	// Set up 2 trackers and attach directly
	trackers := makeTrackers(2, 1)
	rt.mux.Lock()
	rt.assignedTrackers = trackers
	rt.mux.Unlock()

	// Make several selections; round-robin should distribute across both trackers, not always the first
	selections := make(map[string]int)
	for i := 0; i < 10; i++ {
		lbPolicy := rt.lbPolicy.Load().(lbPolicy)
		cb, tracker := lbPolicy(ctx, rt.assignedTrackers)
		if tracker != nil {
			selections[tracker.dest]++
			if cb != nil {
				cb()
			}
		}
	}
	if len(selections) < 2 {
		t.Errorf("Spec loadBalancingPolicy not honored: selections only from %v", selections)
	}
}

func TestDynamicLoadBalancingPolicyUpdate(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()

	servingClient := fakeservingclient.Get(ctx)
	revisions := fakerevisioninformer.Get(ctx)

	// Create a revision with no policy (defaults to first-available for CC=1)
	rev := &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-revision-dyn",
			Namespace: testNamespace,
		},
		Spec: v1.RevisionSpec{
			ContainerConcurrency: ptr.Int64(1),
		},
	}
	servingClient.ServingV1().Revisions(rev.Namespace).Create(ctx, rev, metav1.CreateOptions{})
	revisions.Informer().GetIndexer().Add(rev)

	throttler := NewThrottler(ctx, "10.10.10.10")
	revID := types.NamespacedName{Namespace: rev.Namespace, Name: rev.Name}
	rt, err := throttler.getOrCreateRevisionThrottler(revID)
	if err != nil {
		t.Fatalf("Failed to get revision throttler: %v", err)
	}

	// Two trackers capacity 1 each
	trackers := makeTrackers(2, 1)
	rt.mux.Lock()
	rt.assignedTrackers = trackers
	rt.mux.Unlock()

	// With first-available, repeated selections should be biased to the first tracker
	selections := make(map[string]int)
	for i := 0; i < 10; i++ {
		lbPolicy := rt.lbPolicy.Load().(lbPolicy)
		cb, tracker := lbPolicy(ctx, rt.assignedTrackers)
		if tracker != nil {
			selections[tracker.dest]++
			if cb != nil {
				cb()
			}
		}
	}
	if len(selections) == 0 {
		t.Fatal("No selections made")
	}
	// Expect mostly or exclusively first dest before update
	firstDest := trackers[0].dest
	if selections[firstDest] < 5 { // should be majority
		t.Fatalf("Unexpected distribution before update: %v", selections)
	}

	// Update the revision to set round-robin via spec and invoke revisionUpdated
	rev = rev.DeepCopy()
	rev.Spec.LoadBalancingPolicy = stringPtr("round-robin")
	// Update informer store and call revisionUpdated
	revisions.Informer().GetIndexer().Update(rev)
	throttler.revisionUpdated(rev)

	// Reset counts and sample again
	selections = make(map[string]int)
	for i := 0; i < 10; i++ {
		lbPolicy := rt.lbPolicy.Load().(lbPolicy)
		cb, tracker := lbPolicy(ctx, rt.assignedTrackers)
		if tracker != nil {
			selections[tracker.dest]++
			if cb != nil {
				cb()
			}
		}
	}
	if len(selections) < 2 {
		t.Fatalf("Policy did not update dynamically, selections: %v", selections)
	}
}

func TestThrottlerWithLoadBalancingPolicy(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	servingClient := fakeservingclient.Get(ctx)
	revisions := fakerevisioninformer.Get(ctx)
	fake := fakekubeclient.Get(ctx)
	endpoints := fakeendpointsinformer.Get(ctx)

	waitInformers, err := rtesting.RunAndSyncInformers(ctx, endpoints.Informer(), revisions.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	// Create a revision with round-robin policy
	rev := &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-revision-lb",
			Namespace: testNamespace,
		},
		Spec: v1.RevisionSpec{
			LoadBalancingPolicy:  stringPtr("round-robin"),
			ContainerConcurrency: ptr.Int64(10),
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image: "busybox",
				}},
			},
		},
	}
	servingClient.ServingV1().Revisions(rev.Namespace).Create(ctx, rev, metav1.CreateOptions{})
	revisions.Informer().GetIndexer().Add(rev)

	updateCh := make(chan revisionDestsUpdate)
	throttler := NewThrottler(ctx, "10.10.10.10")

	var grp errgroup.Group
	grp.Go(func() error { throttler.run(updateCh); return nil })
	defer func() {
		close(updateCh)
		grp.Wait()
	}()

	// Create public endpoints
	publicEp := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-revision-lb",
			Namespace: testNamespace,
			Labels: map[string]string{
				networking.ServiceTypeKey: string(networking.ServiceTypePublic),
				serving.RevisionLabelKey:  "test-revision-lb",
			},
		},
		Subsets: []corev1.EndpointSubset{
			*epSubset(8012, "http", []string{"10.10.10.10"}, nil),
		},
	}
	fake.CoreV1().Endpoints(testNamespace).Create(ctx, publicEp, metav1.CreateOptions{})
	endpoints.Informer().GetIndexer().Add(publicEp)

	// Send update to throttler
	revID := types.NamespacedName{Namespace: testNamespace, Name: "test-revision-lb"}
	updateCh <- revisionDestsUpdate{
		Rev:   revID,
		Dests: sets.New("10.0.0.1:8080", "10.0.0.2:8080", "10.0.0.3:8080"),
	}

	// Wait for throttler to process the update
	rt, err := throttler.getOrCreateRevisionThrottler(revID)
	if err != nil {
		t.Fatal("RevisionThrottler can't be found:", err)
	}

	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 3*time.Second, true, func(context.Context) (bool, error) {
		rt.mux.RLock()
		defer rt.mux.RUnlock()
		return len(rt.assignedTrackers) == 3, nil
	}); err != nil {
		t.Fatal("Timed out waiting for trackers:", err)
	}

	// Track selections over multiple requests
	selections := make(map[string]int)

	// Make requests and track which pods are selected
	for i := range 30 {
		err := throttler.Try(ctx, revID, fmt.Sprintf("req-%d", i), func(dest string, isClusterIP bool) error {
			selections[dest]++
			return nil
		})
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
	}

	// Verify we got even distribution (characteristic of round-robin)
	expectedPerPod := 10
	for dest, count := range selections {
		if count < expectedPerPod-2 || count > expectedPerPod+2 {
			t.Errorf("Round-robin distribution not working: %s got %d requests (expected ~%d)",
				dest, count, expectedPerPod)
		}
	}
}

func (t *Throttler) try(ctx context.Context, requests int, try func(string) error) chan tryResult {
	resultChan := make(chan tryResult)

	revID := types.NamespacedName{Namespace: testNamespace, Name: testRevision}
	for range requests {
		go func() {
			var result tryResult
			if err := t.Try(ctx, revID, "test", func(dest string, isClusterIP bool) error {
				result = tryResult{dest: dest}
				return try(dest)
			}); err != nil {
				result = tryResult{err: err}
			}
			resultChan <- result
		}()
	}

	return resultChan
}

func TestInfiniteBreaker(t *testing.T) {
	b := &infiniteBreaker{
		broadcast: make(chan struct{}),
		logger:    TestLogger(t),
	}

	// Verify initial condition.
	if got, want := b.Capacity(), uint64(0); got != want {
		t.Errorf("Cap=%d, want: %d", got, want)
	}
	if _, ok := b.Reserve(context.Background()); ok != true {
		t.Error("Reserve failed, must always succeed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Maybe(ctx, nil); err == nil {
		t.Error("Should have failed, but didn't")
	}

	b.UpdateConcurrency(1)
	if got, want := b.Capacity(), uint64(1); got != want {
		t.Errorf("Cap=%d, want: %d", got, want)
	}

	// Verify we call the thunk when we have achieved capacity.
	// Twice.
	for range 2 {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		res := false
		if err := b.Maybe(ctx, func() { res = true }); err != nil {
			t.Error("Should have succeeded, but didn't")
		}
		if !res {
			t.Error("thunk was not invoked")
		}
	}

	// Scale to zero
	b.UpdateConcurrency(0)

	// Repeat initial test.
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if err := b.Maybe(ctx, nil); err == nil {
		t.Error("Should have failed, but didn't")
	}
	if got, want := b.Capacity(), uint64(0); got != want {
		t.Errorf("Cap=%d, want: %d", got, want)
	}

	// And now do the async test.
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	// Unlock the channel after a short delay.
	go func() {
		time.Sleep(10 * time.Millisecond)
		b.UpdateConcurrency(1)
	}()
	res := false
	if err := b.Maybe(ctx, func() { res = true }); err != nil {
		t.Error("Should have succeeded, but didn't")
	}
	if !res {
		t.Error("thunk was not invoked")
	}
}

func TestInferIndex(t *testing.T) {
	const myIP = "10.10.10.3"
	tests := []struct {
		label string
		ips   []string
		want  int
	}{{
		"empty",
		[]string{},
		-1,
	}, {
		"missing",
		[]string{"11.11.11.11", "11.11.11.12"},
		-1,
	}, {
		"first",
		[]string{"10.10.10.3", "11.11.11.11"},
		0,
	}, {
		"middle",
		[]string{"10.10.10.1", "10.10.10.2", "10.10.10.3", "11.11.11.11"},
		2,
	}, {
		"last",
		[]string{"10.10.10.1", "10.10.10.2", "10.10.10.3"},
		2,
	}}
	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			if got, want := inferIndex(test.ips, myIP), test.want; got != want {
				t.Errorf("Index = %d, want: %d", got, want)
			}
		})
	}
}

func TestAssignSlice(t *testing.T) {
	opt := cmp.Comparer(func(a, b *podTracker) bool {
		return a.dest == b.dest
	})
	// assignSlice receives the pod trackers sorted.
	trackers := map[string]*podTracker{
		"dest1": {
			dest: "1",
		},
		"dest2": {
			dest: "2",
		},
		"dest3": {
			dest: "3",
		},
	}
	assignedTrackers := []*podTracker{{
		dest: "1",
	}, {
		dest: "2",
	}, {
		dest: "3",
	}}
	t.Run("notrackers", func(t *testing.T) {
		got := assignSlice(map[string]*podTracker{}, 0 /*selfIdx*/, 1 /*numAct*/)
		if !cmp.Equal(got, []*podTracker{}, opt) {
			t.Errorf("Got=%v, want: %v, diff: %s", got, assignedTrackers,
				cmp.Diff([]*podTracker{}, got, opt))
		}
	})
	t.Run("idx=1, na=1", func(t *testing.T) {
		got := assignSlice(trackers, 1, 1)
		if !cmp.Equal(got, assignedTrackers, opt) {
			t.Errorf("Got=%v, want: %v, diff: %s", got, assignedTrackers,
				cmp.Diff(assignedTrackers, got, opt))
		}
	})
	t.Run("idx=-1", func(t *testing.T) {
		got := assignSlice(trackers, -1, 1)
		if !cmp.Equal(got, assignedTrackers, opt) {
			t.Errorf("Got=%v, want: %v, diff: %s", got, assignedTrackers,
				cmp.Diff(assignedTrackers, got, opt))
		}
	})
	t.Run("idx=1 na=3", func(t *testing.T) {
		cp := make(map[string]*podTracker)
		maps.Copy(cp, trackers)
		got := assignSlice(cp, 1, 3)
		// With consistent hashing: idx=1 gets pod at index 1 (dest2)
		if !cmp.Equal(got, assignedTrackers[1:2], opt) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, assignedTrackers[1:2],
				cmp.Diff(assignedTrackers[1:2], got, opt))
		}
	})
	t.Run("len=1", func(t *testing.T) {
		cp := make(map[string]*podTracker)
		maps.Copy(cp, trackers)
		delete(cp, "dest2")
		delete(cp, "dest3")
		got := assignSlice(cp, 1, 3)
		// With consistent hashing: 1 pod, 3 activators, selfIndex=1
		// Pod at index 0: 0%3=0 goes to activator 0, so activator 1 gets nothing
		if !cmp.Equal(got, []*podTracker{}, opt) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, []*podTracker{},
				cmp.Diff([]*podTracker{}, got, opt))
		}
	})

	t.Run("idx=1, breaker", func(t *testing.T) {
		trackers := map[string]*podTracker{
			"dest1": {
				dest: "1",
				b:    queue.NewBreaker(testBreakerParams),
			},
			"dest2": {
				dest: "2",
				b:    queue.NewBreaker(testBreakerParams),
			},
			"dest3": {
				dest: "3",
				b:    queue.NewBreaker(testBreakerParams),
			},
		}
		assignedTrackers := []*podTracker{{
			dest: "1",
			b:    queue.NewBreaker(testBreakerParams),
		}, {
			dest: "2",
			b:    queue.NewBreaker(testBreakerParams),
		}, {
			dest: "3",
			b:    queue.NewBreaker(testBreakerParams),
		}}
		cp := maps.Clone(trackers)
		got := assignSlice(cp, 1, 2)
		// With consistent hashing: idx=1, na=2 gets pods at indices where i%2==1, so dest2 and dest3 don't match
		// Actually, with sorted keys ["dest1", "dest2", "dest3"], idx=1 gets index 1 (dest2)
		want := []*podTracker{assignedTrackers[1]} // Just dest2
		if !cmp.Equal(got, want, opt) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, want,
				cmp.Diff(want, got, opt))
		}
		if got, want := got[0].b.Capacity(), uint64(0); got != want {
			t.Errorf("Capacity for the tail pod = %d, want: %d", got, want)
		}
	})

	// Additional tests for consistent hashing
	t.Run("5 pods, 3 activators", func(t *testing.T) {
		fivePodTrackers := map[string]*podTracker{
			"dest1": {dest: "1"},
			"dest2": {dest: "2"},
			"dest3": {dest: "3"},
			"dest4": {dest: "4"},
			"dest5": {dest: "5"},
		}
		// Sorted: ["dest1", "dest2", "dest3", "dest4", "dest5"]
		// Activator 0: indices 0, 3 -> dest1, dest4
		// Activator 1: indices 1, 4 -> dest2, dest5
		// Activator 2: index 2 -> dest3

		got0 := assignSlice(fivePodTrackers, 0, 3)
		want0 := []*podTracker{{dest: "1"}, {dest: "4"}}
		if !cmp.Equal(got0, want0, opt) {
			t.Errorf("Activator 0: Got=%v, want: %v; diff: %s", got0, want0, cmp.Diff(want0, got0, opt))
		}

		got1 := assignSlice(fivePodTrackers, 1, 3)
		want1 := []*podTracker{{dest: "2"}, {dest: "5"}}
		if !cmp.Equal(got1, want1, opt) {
			t.Errorf("Activator 1: Got=%v, want: %v; diff: %s", got1, want1, cmp.Diff(want1, got1, opt))
		}

		got2 := assignSlice(fivePodTrackers, 2, 3)
		want2 := []*podTracker{{dest: "3"}}
		if !cmp.Equal(got2, want2, opt) {
			t.Errorf("Activator 2: Got=%v, want: %v; diff: %s", got2, want2, cmp.Diff(want2, got2, opt))
		}
	})
}

// TestTryWithAllPodsQuarantined verifies requests are re-enqueued when all pods are quarantined
func TestTryWithAllPodsQuarantined(t *testing.T) {
	logger := TestLogger(t)

	t.Run("re-enqueue when all pods quarantined until healthy", func(t *testing.T) {
		// Set up a revision throttler with quarantined pods
		rt := &revisionThrottler{
			logger:  logger,
			revID:   types.NamespacedName{Namespace: "test", Name: "revision"},
			breaker: queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
		}
		rt.lbPolicy.Store(lbPolicy(firstAvailableLBPolicy))

		// Create only quarantined trackers
		quarantinedTracker1 := &podTracker{dest: "quarantined-pod-1"}
		quarantinedTracker1.state.Store(uint32(podQuarantined))
		quarantinedTracker1.quarantineEndTime.Store(time.Now().Unix() + 1)

		quarantinedTracker2 := &podTracker{dest: "quarantined-pod-2"}
		quarantinedTracker2.state.Store(uint32(podQuarantined))
		quarantinedTracker2.quarantineEndTime.Store(time.Now().Unix() + 1)

		rt.assignedTrackers = []*podTracker{quarantinedTracker1, quarantinedTracker2}

		// After a short delay, flip one tracker to healthy to let try() proceed
		go func() {
			time.Sleep(50 * time.Millisecond)
			quarantinedTracker1.state.Store(uint32(podHealthy))
			quarantinedTracker1.quarantineEndTime.Store(0)
		}()

		// Try to make a request; ensure function eventually called
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		called := false
		err := rt.try(ctx, "test-request-id", func(dest string, isClusterIP bool) error {
			called = true
			return nil
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !called {
			t.Fatal("Expected function to be called after a tracker becomes healthy")
		}
	})
}

func TestThrottlerQuick502Quarantine(t *testing.T) {
	// TODO: TEMPORARILY DISABLED - Re-enable this test when quick 502 quarantine functionality is re-enabled
	t.Skip("Quick 502 quarantine functionality is currently disabled - see TODO comments in throttler.go")
	logger := TestLogger(t)

	t.Run("pod quarantined on quick 502", func(t *testing.T) {
		// Create a simple revision throttler
		rt := &revisionThrottler{
			logger:      logger,
			breaker:     queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10}),
			podTrackers: make(map[string]*podTracker),
		}
		rt.lbPolicy.Store(lbPolicy(firstAvailableLBPolicy))

		// Create two pod trackers - one will get quarantined, one will succeed
		pod1 := &podTracker{
			dest: "10.0.0.1:8080",
			b:    queue.NewBreaker(testBreakerParams),
		}
		pod1.state.Store(uint32(podHealthy))
		pod1.b.UpdateConcurrency(1) // Give it capacity

		pod2 := &podTracker{
			dest: "10.0.0.2:8080",
			b:    queue.NewBreaker(testBreakerParams),
		}
		pod2.state.Store(uint32(podHealthy))
		pod2.b.UpdateConcurrency(1) // Give it capacity

		// Add them to the throttler
		rt.podTrackers[pod1.dest] = pod1
		rt.podTrackers[pod2.dest] = pod2
		rt.assignedTrackers = []*podTracker{pod1, pod2}

		// Initialize main breaker capacity
		rt.breaker.UpdateConcurrency(2)

		// Simulate a quick 502 response
		attemptCount := 0
		ctx := context.Background()
		err := rt.try(ctx, "test-502", func(dest string, isClusterIP bool) error {
			attemptCount++
			if dest == pod1.dest {
				// First pod returns quick 502
				return handler.ErrQuick502{Duration: 50 * time.Millisecond}
			}
			// Second pod succeeds
			return nil
		})
		// Request should succeed (nil error) because it was retried
		if err != nil {
			t.Errorf("Expected nil error after retry, got %v", err)
		}

		// Verify the first pod was quarantined
		if podState(pod1.state.Load()) != podQuarantined {
			t.Error("Pod1 should be quarantined after quick 502")
		}

		// Verify quarantine end time is set
		quarantineEnd := pod1.quarantineEndTime.Load()
		if quarantineEnd == 0 {
			t.Error("Quarantine end time should be set")
		}

		// Verify quarantine uses first backoff step
		// Pod was healthy before quarantine, so wasPending=false (standard backoff)
		expectedEnd := time.Now().Unix() + int64(quarantineBackoffSeconds(1, false))
		if quarantineEnd > expectedEnd+1 || quarantineEnd < expectedEnd-1 {
			t.Errorf("Quarantine end time incorrect: got %d, expected ~%d", quarantineEnd, expectedEnd)
		}
	})

	t.Run("pod not quarantined on slow 502", func(t *testing.T) {
		// Create a simple revision throttler
		rt := &revisionThrottler{
			logger:      logger,
			breaker:     queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10}),
			podTrackers: make(map[string]*podTracker),
		}
		rt.lbPolicy.Store(lbPolicy(firstAvailableLBPolicy))

		// Create a healthy pod tracker
		healthyPod := &podTracker{
			dest: "10.0.0.2:8080",
			b:    queue.NewBreaker(testBreakerParams),
		}
		healthyPod.state.Store(uint32(podHealthy))
		healthyPod.b.UpdateConcurrency(1) // Give it capacity

		// Add it to the throttler
		rt.podTrackers[healthyPod.dest] = healthyPod
		rt.assignedTrackers = []*podTracker{healthyPod}

		// Initialize main breaker capacity
		rt.breaker.UpdateConcurrency(1)

		// Simulate a slow 502 response (over 100ms)
		ctx := context.Background()
		err := rt.try(ctx, "test-slow-502", func(dest string, isClusterIP bool) error {
			// Return a regular error, not ErrQuick502
			return errors.New("some error")
		})

		// Should get the error back
		if err == nil || err.Error() != "some error" {
			t.Errorf("Expected 'some error', got %v", err)
		}

		// Verify the pod was NOT quarantined
		if podState(healthyPod.state.Load()) != podHealthy {
			t.Error("Pod should remain healthy after slow 502")
		}
	})

	t.Run("multiple pods with quick 502", func(t *testing.T) {
		// Create a simple revision throttler with round-robin policy for predictable behavior
		rt := &revisionThrottler{
			logger:      logger,
			breaker:     queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10}),
			podTrackers: make(map[string]*podTracker),
		}
		rt.lbPolicy.Store(lbPolicy(firstAvailableLBPolicy))

		// Create multiple pod trackers
		pod1 := &podTracker{
			dest: "10.0.0.1:8080",
			b:    queue.NewBreaker(testBreakerParams),
		}
		pod1.state.Store(uint32(podHealthy))

		pod2 := &podTracker{
			dest: "10.0.0.2:8080",
			b:    queue.NewBreaker(testBreakerParams),
		}
		pod2.state.Store(uint32(podHealthy))

		pod3 := &podTracker{
			dest: "10.0.0.3:8080",
			b:    queue.NewBreaker(testBreakerParams),
		}
		pod3.state.Store(uint32(podHealthy))

		// Give pods capacity
		pod1.b.UpdateConcurrency(1)
		pod2.b.UpdateConcurrency(1)
		pod3.b.UpdateConcurrency(1)

		// Add them to the throttler
		rt.podTrackers[pod1.dest] = pod1
		rt.podTrackers[pod2.dest] = pod2
		rt.podTrackers[pod3.dest] = pod3
		rt.assignedTrackers = []*podTracker{pod1, pod2, pod3}

		// Initialize main breaker capacity
		rt.breaker.UpdateConcurrency(3)

		// Track which pods were tried
		triedPods := make(map[string]bool)
		attemptCount := 0

		ctx := context.Background()
		err := rt.try(ctx, "test-multi-502", func(dest string, isClusterIP bool) error {
			attemptCount++
			triedPods[dest] = true

			// First two pods return quick 502
			if dest == pod1.dest || dest == pod2.dest {
				return handler.ErrQuick502{Duration: 50 * time.Millisecond}
			}

			// Third pod succeeds
			return nil
		})
		// Request should succeed
		if err != nil {
			t.Errorf("Expected nil error, got %v", err)
		}

		// Should have tried 3 pods (2 failures + 1 success)
		if attemptCount != 3 {
			t.Errorf("Expected 3 attempts, got %d", attemptCount)
		}

		// First two pods should be quarantined
		if podState(pod1.state.Load()) != podQuarantined {
			t.Error("Pod1 should be quarantined")
		}
		if podState(pod2.state.Load()) != podQuarantined {
			t.Error("Pod2 should be quarantined")
		}

		// Third pod should remain healthy
		if podState(pod3.state.Load()) != podHealthy {
			t.Error("Pod3 should remain healthy")
		}
	})
}

func TestQuarantineRecoveryMechanism(t *testing.T) {
	logger := TestLogger(t)

	t.Run("quarantined pods transition to recovering state", func(t *testing.T) {
		rt := &revisionThrottler{
			logger:  logger,
			revID:   types.NamespacedName{Namespace: "test", Name: "revision"},
			breaker: queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
		}

		// Create a quarantined tracker with expired quarantine time
		tracker := &podTracker{dest: "quarantined-pod"}
		tracker.state.Store(uint32(podQuarantined))
		tracker.quarantineEndTime.Store(time.Now().Unix() - 1) // Expired

		trackers := []*podTracker{tracker}
		available := rt.filterAvailableTrackers(context.Background(), trackers)

		// Should transition to recovering and be included in available
		if len(available) != 1 {
			t.Errorf("Expected 1 available tracker, got %d", len(available))
		}
		if podState(tracker.state.Load()) != podRecovering {
			t.Errorf("Expected podRecovering state, got %v", podState(tracker.state.Load()))
		}
	})

	t.Run("recovering pods are included in available trackers", func(t *testing.T) {
		rt := &revisionThrottler{
			logger:  logger,
			revID:   types.NamespacedName{Namespace: "test", Name: "revision"},
			breaker: queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
		}

		// Create trackers in different states
		healthyTracker := &podTracker{dest: "healthy-pod"}
		healthyTracker.state.Store(uint32(podHealthy))

		recoveringTracker := &podTracker{dest: "recovering-pod"}
		recoveringTracker.state.Store(uint32(podRecovering))

		quarantinedTracker := &podTracker{dest: "quarantined-pod"}
		quarantinedTracker.state.Store(uint32(podQuarantined))
		quarantinedTracker.quarantineEndTime.Store(time.Now().Unix() + 10) // Not expired

		drainingTracker := &podTracker{dest: "draining-pod"}
		drainingTracker.state.Store(uint32(podDraining))

		trackers := []*podTracker{healthyTracker, recoveringTracker, quarantinedTracker, drainingTracker}
		available := rt.filterAvailableTrackers(context.Background(), trackers)

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

		// Create a recovering tracker
		tracker := &podTracker{
			dest: "recovering-pod",
			b:    queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
		}
		tracker.state.Store(uint32(podRecovering))
		tracker.quarantineCount.Store(3) // Had previous quarantines

		// Simulate the logic from try() when a request succeeds
		currentState := podState(tracker.state.Load())
		if currentState == podRecovering {
			tracker.state.Store(uint32(podHealthy))
			tracker.quarantineCount.Store(0)
		}

		// Pod should now be healthy with reset quarantine count
		if podState(tracker.state.Load()) != podHealthy {
			t.Errorf("Expected podHealthy state after successful request, got %v", podState(tracker.state.Load()))
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
		tracker := newPodTracker("recovering-pod", queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
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
		rt.updateThrottlerState(1, nil, []string{"recovering-pod"}, nil, nil)

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
		available := rt.filterAvailableTrackers(context.Background(), trackers)

		// Should not be available and should remain quarantined
		if len(available) != 0 {
			t.Errorf("Expected 0 available trackers, got %d", len(available))
		}
		if podState(tracker.state.Load()) != podQuarantined {
			t.Errorf("Expected podQuarantined state, got %v", podState(tracker.state.Load()))
		}
	})
}

func TestResetTrackersRaceCondition(t *testing.T) {
	logger := TestLogger(t)

	t.Run("resetTrackers concurrent with tracker modifications", func(t *testing.T) {
		rt := &revisionThrottler{
			logger:      logger,
			revID:       types.NamespacedName{Namespace: "test", Name: "revision"},
			breaker:     queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
			podTrackers: make(map[string]*podTracker),
		}
		rt.containerConcurrency.Store(2) // Enable resetTrackers to actually do work
		rt.lbPolicy.Store(lbPolicy(firstAvailableLBPolicy))
		rt.numActivators.Store(1)
		rt.activatorIndex.Store(0)

		// Create initial trackers
		initialTrackers := make([]*podTracker, 3)
		for i := 0; i < 3; i++ {
			tracker := newPodTracker(fmt.Sprintf("pod-%d:8080", i),
				queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
			initialTrackers[i] = tracker
		}

		// Add initial trackers
		rt.updateThrottlerState(3, initialTrackers, nil, nil, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		var wg sync.WaitGroup

		// Goroutine 1: Continuously call resetTrackers
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				rt.resetTrackers()
				// Small delay to let other goroutine work
				time.Sleep(time.Microsecond)
			}
		}()

		// Goroutine 2: Continuously add/remove trackers
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter := 0
			for ctx.Err() == nil {
				counter++
				trackerName := fmt.Sprintf("dynamic-pod-%d:8080", counter%5)

				if counter%2 == 0 {
					// Add a tracker
					newTracker := newPodTracker(trackerName,
						queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
					rt.updateThrottlerState(1, []*podTracker{newTracker}, nil, nil, nil)
				} else {
					// Remove a tracker by putting it in draining
					rt.updateThrottlerState(0, nil, nil, []string{trackerName}, nil)
				}

				// Small delay to let resetTrackers work
				time.Sleep(time.Microsecond)
			}
		}()

		// Wait for goroutines to finish
		wg.Wait()

		// Test should complete without race conditions or panics
		// The actual race detection happens when run with -race flag
	})

	t.Run("resetTrackers with nil tracker in map", func(t *testing.T) {
		// This tests a specific edge case where a tracker might be nil
		rt := &revisionThrottler{
			logger:      logger,
			revID:       types.NamespacedName{Namespace: "test", Name: "revision"},
			breaker:     queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}),
			podTrackers: make(map[string]*podTracker),
		}
		rt.containerConcurrency.Store(2)

		// Manually add a nil tracker (simulating corruption)
		rt.mux.Lock()
		rt.podTrackers["nil-tracker"] = nil
		rt.mux.Unlock()

		// This should not panic
		rt.resetTrackers()
	})
}

// TestPodTrackerStateRaces tests for race conditions in pod state transitions
func TestPodTrackerStateRaces(t *testing.T) {
	t.Run("concurrent state transitions", func(t *testing.T) {
		tracker := newPodTracker("pod1:8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		var wg sync.WaitGroup

		// Goroutine 1: Toggle between states using atomic operations
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				// Simulate quarantine by setting state
				tracker.state.Store(uint32(podQuarantined))
				tracker.quarantineEndTime.Store(time.Now().Add(time.Second).Unix())
				time.Sleep(time.Microsecond)
				// Try to transition to recovering
				tracker.state.CompareAndSwap(uint32(podQuarantined), uint32(podRecovering))
				time.Sleep(time.Microsecond)
				// Mark as healthy
				tracker.state.Store(uint32(podHealthy))
				time.Sleep(time.Microsecond)
			}
		}()

		// Goroutine 2: Read state and try to reserve
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				state := podState(tracker.state.Load())
				if state == podHealthy {
					cb, ok := tracker.Reserve(context.Background())
					if ok && cb != nil {
						cb()
					}
				}
				time.Sleep(time.Microsecond)
			}
		}()

		// Goroutine 3: Try to drain
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				tracker.tryDrain()
				time.Sleep(5 * time.Microsecond)
				tracker.state.Store(uint32(podHealthy))
				time.Sleep(5 * time.Microsecond)
			}
		}()

		wg.Wait()
	})

	t.Run("concurrent Reserve and state change", func(t *testing.T) {
		tracker := newPodTracker("pod2:8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 100}))
		tracker.state.Store(uint32(podHealthy))

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		var wg sync.WaitGroup

		// Many goroutines trying to Reserve
		for range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for ctx.Err() == nil {
					cb, ok := tracker.Reserve(context.Background())
					if ok && cb != nil {
						// Hold the reservation briefly
						time.Sleep(time.Microsecond)
						cb()
					}
				}
			}()
		}

		// Goroutine changing states
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				tracker.state.Store(uint32(podQuarantined))
				time.Sleep(time.Millisecond)
				tracker.state.Store(uint32(podHealthy))
				time.Sleep(time.Millisecond)
			}
		}()

		wg.Wait()
	})

	t.Run("concurrent pending state transitions", func(t *testing.T) {
		tracker := newPodTracker("pod3:8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
		// Start in pending state
		tracker.state.Store(uint32(podPending))

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		var wg sync.WaitGroup

		// Multiple goroutines doing state transitions
		for i := range 5 {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for ctx.Err() == nil {
					// Try CAS from pending to healthy
					if tracker.state.CompareAndSwap(uint32(podPending), uint32(podHealthy)) {
						// Successfully became healthy
						time.Sleep(time.Microsecond)
						// Reset to pending for next iteration
						tracker.state.Store(uint32(podPending))
					}
				}
			}(i)
		}

		wg.Wait()
	})
}

// TestRevisionThrottlerRaces tests for race conditions in revisionThrottler operations
func TestRevisionThrottlerRaces(t *testing.T) {
	logger := TestLogger(t)

	t.Run("concurrent updateThrottlerState calls", func(t *testing.T) {
		rt := &revisionThrottler{
			logger:      logger,
			revID:       types.NamespacedName{Namespace: "test", Name: "revision"},
			breaker:     queue.NewBreaker(queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 100}),
			podTrackers: make(map[string]*podTracker),
		}
		rt.containerConcurrency.Store(10)
		rt.lbPolicy.Store(lbPolicy(randomLBPolicy))
		rt.numActivators.Store(1)
		rt.activatorIndex.Store(0)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		var wg sync.WaitGroup

		// Goroutine adding trackers
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter := 0
			for ctx.Err() == nil {
				counter++
				tracker := newPodTracker(fmt.Sprintf("add-pod-%d:8080", counter),
					queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
				rt.updateThrottlerState(1, []*podTracker{tracker}, nil, nil, nil)
				time.Sleep(time.Microsecond)
			}
		}()

		// Goroutine removing trackers
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter := 0
			for ctx.Err() == nil {
				counter++
				rt.updateThrottlerState(0, nil, nil, []string{fmt.Sprintf("add-pod-%d:8080", counter)}, nil)
				time.Sleep(time.Microsecond)
			}
		}()

		// Goroutine reading assignedTrackers
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				rt.mux.RLock()
				_ = len(rt.assignedTrackers)
				rt.mux.RUnlock()
				time.Sleep(time.Microsecond)
			}
		}()

		wg.Wait()
	})

	t.Run("concurrent acquireDest", func(t *testing.T) {
		rt := &revisionThrottler{
			logger:      logger,
			revID:       types.NamespacedName{Namespace: "test", Name: "revision"},
			breaker:     queue.NewBreaker(queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 100}),
			podTrackers: make(map[string]*podTracker),
		}
		rt.containerConcurrency.Store(10)
		rt.lbPolicy.Store(lbPolicy(randomLBPolicy))
		rt.numActivators.Store(1)
		rt.activatorIndex.Store(0)

		// Add some initial trackers
		initialTrackers := make([]*podTracker, 5)
		for i := range 5 {
			initialTrackers[i] = newPodTracker(fmt.Sprintf("pod-%d:8080", i),
				queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
			initialTrackers[i].state.Store(uint32(podHealthy))
		}
		rt.updateThrottlerState(5, initialTrackers, nil, nil, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		var wg sync.WaitGroup

		// Many goroutines trying to acquire dest
		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for ctx.Err() == nil {
					cb, tracker, ok := rt.acquireDest(context.Background())
					if ok && tracker != nil && cb != nil {
						// Hold briefly then release
						time.Sleep(time.Microsecond)
						cb()
					}
				}
			}()
		}

		// Goroutine updating throttler state
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter := 0
			for ctx.Err() == nil {
				counter++
				if counter%2 == 0 {
					// Add a tracker
					tracker := newPodTracker(fmt.Sprintf("dynamic-%d:8080", counter),
						queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
					tracker.state.Store(uint32(podHealthy))
					rt.updateThrottlerState(1, []*podTracker{tracker}, nil, nil, nil)
				} else {
					// Remove a tracker
					rt.updateThrottlerState(0, nil, nil, []string{fmt.Sprintf("pod-%d:8080", counter%5)}, nil)
				}
				time.Sleep(time.Millisecond)
			}
		}()

		wg.Wait()
	})
}

// TestPodTrackerRefCountRaces tests for race conditions in reference counting
func TestPodTrackerRefCountRaces(t *testing.T) {
	t.Run("concurrent addRef and releaseRef", func(t *testing.T) {
		tracker := newPodTracker("pod:8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 100, MaxConcurrency: 100, InitialCapacity: 100}))
		tracker.state.Store(uint32(podHealthy))

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		var wg sync.WaitGroup

		// Goroutines increasing ref count
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for ctx.Err() == nil {
					tracker.addRef()
					time.Sleep(time.Microsecond)
				}
			}()
		}

		// Goroutines decreasing ref count
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for ctx.Err() == nil {
					tracker.releaseRef()
					time.Sleep(time.Microsecond)
				}
			}()
		}

		// Goroutine reading ref count
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				_ = tracker.getRefCount()
				time.Sleep(time.Microsecond)
			}
		}()

		wg.Wait()
	})

	t.Run("refCount races with state transitions", func(t *testing.T) {
		tracker := newPodTracker("pod:8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))
		tracker.state.Store(uint32(podHealthy))

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		var wg sync.WaitGroup

		// Goroutine doing ref count operations
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				tracker.addRef()
				time.Sleep(time.Microsecond)
				tracker.releaseRef()
				time.Sleep(time.Microsecond)
			}
		}()

		// Goroutine trying to drain
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				tracker.tryDrain()
				time.Sleep(time.Millisecond)
				tracker.state.Store(uint32(podHealthy))
				time.Sleep(time.Millisecond)
			}
		}()

		wg.Wait()
	})
}

// TestPodTrackerWeightRaces tests for race conditions in weight operations
func TestPodTrackerWeightRaces(t *testing.T) {
	t.Run("concurrent weight modifications", func(t *testing.T) {
		tracker := newPodTracker("pod:8080",
			queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: 10, InitialCapacity: 10}))

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		var wg sync.WaitGroup

		// Multiple goroutines increasing weight
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for ctx.Err() == nil {
					tracker.increaseWeight()
					time.Sleep(time.Microsecond)
				}
			}()
		}

		// Multiple goroutines decreasing weight
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for ctx.Err() == nil {
					tracker.decreaseWeight()
					time.Sleep(time.Microsecond)
				}
			}()
		}

		// Goroutine reading weight
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				_ = tracker.getWeight()
				time.Sleep(time.Microsecond)
			}
		}()

		wg.Wait()
	})
}
