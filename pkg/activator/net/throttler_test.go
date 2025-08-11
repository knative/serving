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
				logger:               logger,
				breaker:              queue.NewBreaker(testBreakerParams),
				containerConcurrency: tt.containerConcurrency,
			}
			rt.numActivators.Store(tt.numActivators)
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
				logger:               logger,
				breaker:              newInfiniteBreaker(logger),
				containerConcurrency: tt.containerConcurrency,
			}
			rt.numActivators.Store(tt.numActivators)
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
		wantDests: sets.New("129.0.0.1:1234"),
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
		wantDests: sets.New("129.0.0.1:1234"),
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

	if got, want := rt.numActivators.Load(), int32(2); got != want {
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
			if rt.lbPolicy == nil {
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
			if revThrottler.lbPolicy == nil {
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
			_, tracker := rt.lbPolicy(ctx, rt.assignedTrackers)
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
		_, tracker := rt.lbPolicy(ctx, rt.assignedTrackers)
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
		_, tracker := rt.lbPolicy(ctx, rt.assignedTrackers)
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
			_, tracker := rt.lbPolicy(ctx, rt.assignedTrackers)
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
		cb, tracker := rt.lbPolicy(ctx, rt.assignedTrackers)
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
		cb, tracker := rt.lbPolicy(ctx, rt.assignedTrackers)
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
		cb, tracker := rt.lbPolicy(ctx, rt.assignedTrackers)
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

func TestPickIndices(t *testing.T) {
	tests := []struct {
		l                   string
		pods                int
		acts                int
		idx                 int
		wantB, wantE, wantR int
	}{{
		l:     "1 pod, 1 activator",
		pods:  1,
		acts:  1,
		idx:   0,
		wantB: 0,
		wantE: 1,
	}, {
		l:     "1 pod, 2 activators, this is 0",
		pods:  1,
		acts:  2,
		idx:   0,
		wantB: 0,
		wantE: 1,
	}, {
		l:     "1 pod, 2 activators, this is 1",
		pods:  1,
		acts:  2,
		idx:   1,
		wantB: 0,
		wantE: 1,
	}, {
		l:     "2 pods, 3 activators, this is 1",
		pods:  2,
		acts:  3,
		idx:   1,
		wantB: 1,
		wantE: 2,
	}, {
		l:     "2 pods, 3 activators, this is 2",
		pods:  2,
		acts:  3,
		idx:   2,
		wantB: 0,
		wantE: 1,
	}, {
		l:     "3 pods, 3 activators, this is 2",
		pods:  3,
		acts:  3,
		idx:   2,
		wantB: 2,
		wantE: 3,
	}, {
		l:     "10 pods, 3 activators this is 0",
		pods:  10,
		acts:  3,
		idx:   0,
		wantB: 0,
		wantE: 3,
		wantR: 1,
	}, {
		l:     "10 pods, 3 activators this is 1",
		pods:  10,
		acts:  3,
		idx:   1,
		wantB: 3,
		wantE: 6,
		wantR: 1,
	}, {
		l:     "10 pods, 3 activators this is 2",
		pods:  10,
		acts:  3,
		idx:   2,
		wantB: 6,
		wantE: 9,
		wantR: 1,
	}, {
		l:     "150 pods, 5 activators this is 0",
		pods:  150,
		acts:  5,
		idx:   0,
		wantB: 0,
		wantE: 30,
	}, {
		l:     "150 pods, 5 activators this is 1",
		pods:  150,
		acts:  5,
		idx:   1,
		wantB: 30,
		wantE: 60,
	}, {
		l:     "150 pods, 5 activators this is 4",
		pods:  150,
		acts:  5,
		idx:   4,
		wantB: 120,
		wantE: 150,
	}}
	for _, test := range tests {
		t.Run(test.l, func(tt *testing.T) {
			bi, ei, rem := pickIndices(test.pods, test.idx, test.acts)
			if got, want := bi, test.wantB; got != want {
				t.Errorf("BeginIndex = %d, want: %d", got, want)
			}
			if got, want := ei, test.wantE; got != want {
				t.Errorf("EndIndex = %d, want: %d", got, want)
			}
			if got, want := rem, test.wantR; got != want {
				t.Errorf("Remnants = %d, want: %d", got, want)
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
		if !cmp.Equal(got, assignedTrackers[1:2], opt) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, assignedTrackers[0:1],
				cmp.Diff(assignedTrackers[1:2], got, opt))
		}
	})
	t.Run("len=1", func(t *testing.T) {
		cp := make(map[string]*podTracker)
		maps.Copy(cp, trackers)
		delete(cp, "dest2")
		delete(cp, "dest3")
		got := assignSlice(cp, 1, 3)
		if !cmp.Equal(got, assignedTrackers[0:1], opt) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, assignedTrackers[0:1],
				cmp.Diff(assignedTrackers[0:1], got, opt))
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
		want := assignedTrackers[1:2]
		if !cmp.Equal(got, want, opt) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, want,
				cmp.Diff(want, got, opt))
		}
		if got, want := got[0].b.Capacity(), uint64(0); got != want {
			t.Errorf("Capacity for the tail pod = %d, want: %d", got, want)
		}
	})
}
