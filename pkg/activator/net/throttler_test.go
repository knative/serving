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
	"math"
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

	"knative.dev/networking/pkg/apis/networking"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeendpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	"knative.dev/pkg/controller"
	. "knative.dev/pkg/logging/testing"
	rtesting "knative.dev/pkg/reconciler/testing"
	_ "knative.dev/pkg/system/testing"
	"knative.dev/serving/pkg/activator/util"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
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
	rt := &revisionThrottler{
		logger:               logger,
		breaker:              queue.NewBreaker(testBreakerParams),
		containerConcurrency: 10,
	}

	rt.updateCapacity(1)
	if got, want := rt.breaker.Capacity(), 1*10; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	rt.updateCapacity(10)
	if got, want := rt.breaker.Capacity(), 10*10; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	rt.updateCapacity(100)
	if got, want := rt.breaker.Capacity(), 100*10; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	rt.numActivators.Store(5)
	rt.updateCapacity(10)
	if got, want := rt.breaker.Capacity(), 10*10/5; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	rt.numActivators.Store(200)
	rt.updateCapacity(10)
	if got, want := rt.breaker.Capacity(), 1; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	rt.updateCapacity(0)
	if got, want := rt.breaker.Capacity(), 0; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// now test with CC=0.
	// in the CC=0 cases we use the infinite breaker whose capacity can either be
	// totally blocked (0) or totally open (1).
	rt.containerConcurrency = 0
	rt.breaker = newInfiniteBreaker(logger)

	rt.updateCapacity(0)
	if got, want := rt.breaker.Capacity(), 0; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	rt.updateCapacity(1)
	if got, want := rt.breaker.Capacity(), 1; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	rt.updateCapacity(10)
	if got, want := rt.breaker.Capacity(), 1; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	rt.numActivators.Store(200)
	rt.updateCapacity(1)
	if got, want := rt.breaker.Capacity(), 1; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	rt.updateCapacity(0)
	if got, want := rt.breaker.Capacity(), 0; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// shouldn't really happen since revisionMaxConcurrency is very, very large,
	// but check that we behave reasonably if it's exceeded.
	capacity := rt.calculateCapacity(revisionMaxConcurrency+5, 1)
	if got, want := capacity, queue.MaxBreakerCapacity; got != want {
		t.Errorf("calculateCapacity = %d, want: %d", got, want)
	}

	// Now test with podIP trackers in tow.
	// Simple case.
	rt.numActivators.Store(1)
	rt.activatorIndex.Store(0)
	rt.podTrackers = makeTrackers(1, 10)
	rt.containerConcurrency = 10
	rt.breaker = queue.NewBreaker(testBreakerParams)

	rt.updateCapacity(0 /* doesn't matter here*/)
	if got, want := rt.breaker.Capacity(), 10; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// 2 backends.
	rt.podTrackers = makeTrackers(2, 10)
	rt.updateCapacity(-1 /* doesn't matter here*/)
	if got, want := rt.breaker.Capacity(), 20; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// 2 activators.
	rt.numActivators.Store(2)
	rt.updateCapacity(-1 /* doesn't matter here*/)
	if got, want := rt.breaker.Capacity(), 10; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// 3 pods, index 0.
	rt.podTrackers = makeTrackers(3, 10)
	rt.updateCapacity(-1 /* doesn't matter here*/)
	if got, want := rt.breaker.Capacity(), 15; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// 3 pods, index 1.
	rt.activatorIndex.Store(1)
	rt.updateCapacity(-1 /* doesn't matter here*/)
	if got, want := rt.breaker.Capacity(), 15; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// Infinite capacity with podIP trackers.
	rt.activatorIndex.Store(1)
	rt.containerConcurrency = 0
	rt.breaker = newInfiniteBreaker(logger)
	rt.podTrackers = makeTrackers(3, 0)
	rt.updateCapacity(1)

	// infinite breaker capacity is either 0 (blocked) or 1 (totally open)
	if got, want := rt.breaker.Capacity(), 1; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	if got, want := len(rt.assignedTrackers), len(rt.podTrackers); got != want {
		t.Errorf("Assigned tracker count = %d, want: %d, diff:\n%s", got, want,
			cmp.Diff(rt.assignedTrackers, rt.podTrackers))
	}
}

func makeTrackers(num, cc int) []*podTracker {
	x := make([]*podTracker, num)
	for i := 0; i < num; i++ {
		x[i] = &podTracker{dest: strconv.Itoa(i)}
		if cc > 0 {
			x[i].b = queue.NewBreaker(queue.BreakerParams{
				QueueDepth:      1,
				MaxConcurrency:  cc,
				InitialCapacity: cc,
			})
		}
	}
	return x
}

func TestThrottlerErrorNoRevision(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	servfake := fakeservingclient.Get(ctx)
	revisions := fakerevisioninformer.Get(ctx)
	waitInformers, err := controller.RunInformers(ctx.Done(), revisions.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	// Add the revision we're testing.
	revID := types.NamespacedName{Namespace: testNamespace, Name: testRevision}
	revision := revisionCC1(revID, networking.ProtocolHTTP1)
	servfake.ServingV1().Revisions(revision.Namespace).Create(revision)
	revisions.Informer().GetIndexer().Add(revision)

	throttler := newTestThrottler(ctx)
	throttler.handleUpdate(revisionDestsUpdate{
		Rev:   revID,
		Dests: sets.NewString("128.0.0.1:1234"),
	})

	// Make sure it now works.
	ctx = util.WithRevID(ctx, revID)
	if err := throttler.Try(ctx, func(string) error { return nil }); err != nil {
		t.Fatalf("Try() = %v, want no error", err)
	}

	// Make sure errors are propagated correctly.
	innerError := errors.New("inner")
	if err := throttler.Try(ctx, func(string) error { return innerError }); err != innerError {
		t.Fatalf("Try() = %v, want %v", err, innerError)
	}

	servfake.ServingV1().Revisions(revision.Namespace).Delete(revision.Name, nil)
	revisions.Informer().GetIndexer().Delete(revID)

	// Eventually it should now fail.
	var lastError error
	wait.PollInfinite(10*time.Millisecond, func() (bool, error) {
		lastError = throttler.Try(ctx, func(string) error { return nil })
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
	waitInformers, err := controller.RunInformers(ctx.Done(), revisions.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	// Add the revision we're testing.
	revID := types.NamespacedName{Namespace: testNamespace, Name: testRevision}
	revision := revisionCC1(revID, networking.ProtocolHTTP1)
	servfake.ServingV1().Revisions(revision.Namespace).Create(revision)
	revisions.Informer().GetIndexer().Add(revision)

	throttler := newTestThrottler(ctx)
	throttler.handleUpdate(revisionDestsUpdate{
		Rev:           revID,
		ClusterIPDest: "129.0.0.1:1234",
		Dests:         sets.NewString("128.0.0.1:1234"),
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
	if result := <-resultChan; result.err != context.DeadlineExceeded {
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
		wantDests   sets.String
	}{{
		name:     "single healthy podIP",
		revision: revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, networking.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.NewString("128.0.0.1:1234"),
		}, {
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.NewString("128.0.0.1:1234"),
		}},
		requests:  1,
		wantDests: sets.NewString("128.0.0.1:1234"),
	}, {
		name:     "single healthy podIP, infinite cc",
		revision: revision(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, networking.ProtocolHTTP1, 0),
		// Double updates exercise additional paths.
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.NewString("128.0.0.2:1234", "128.0.0.32:1212"),
		}, {
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.NewString("128.0.0.1:1234"),
		}},
		requests:  1,
		wantDests: sets.NewString("128.0.0.1:1234"),
	}, {
		name:     "single healthy clusterIP",
		revision: revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, networking.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.NewString("128.0.0.1:1234", "128.0.0.2:1234"),
		}, {
			Rev:           types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			ClusterIPDest: "129.0.0.1:1234",
			Dests:         sets.NewString("128.0.0.1:1234"),
		}},
		requests:  1,
		wantDests: sets.NewString("129.0.0.1:1234"),
	}, {
		name:     "spread podIP load",
		revision: revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, networking.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			// Double update here exercises some additional paths.
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.NewString("128.0.0.3:1234"),
		}, {
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.NewString("128.0.0.1:1234", "128.0.0.2:1234"),
		}},
		requests:  2,
		wantDests: sets.NewString("128.0.0.2:1234", "128.0.0.1:1234"),
	}, {
		name:     "clumping test",
		revision: revision(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, networking.ProtocolHTTP1, 3),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.NewString("128.0.0.1:1234", "128.0.0.2:1234", "128.0.0.2:4236", "128.0.0.2:1233", "128.0.0.2:1230"),
		}},
		requests:  3,
		wantDests: sets.NewString("128.0.0.1:1234"),
	}, {
		name: "roundrobin test",
		revision: revision(types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			networking.ProtocolHTTP1, 5 /*cc >3*/),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			Dests: sets.NewString("128.0.0.1:1234", "128.0.0.2:1234", "211.212.213.214"),
		}},
		requests: 3,
		// All three IP addresses should be used if cc>3.
		wantDests: sets.NewString("128.0.0.1:1234", "128.0.0.2:1234", "211.212.213.214"),
	}, {
		name:     "multiple ClusterIP requests",
		revision: revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, networking.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			Rev:           types.NamespacedName{Namespace: testNamespace, Name: testRevision},
			ClusterIPDest: "129.0.0.1:1234",
			Dests:         sets.NewString("128.0.0.1:1234", "128.0.0.2:1234"),
		}},
		requests:  2,
		wantDests: sets.NewString("129.0.0.1:1234"),
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			servfake := fakeservingclient.Get(ctx)
			fake := fakekubeclient.Get(ctx)
			revisions := fakerevisioninformer.Get(ctx)
			endpoints := fakeendpointsinformer.Get(ctx)

			waitInformers, err := controller.RunInformers(ctx.Done(), endpoints.Informer(),
				revisions.Informer())
			if err != nil {
				t.Fatal("Failed to start informers:", err)
			}
			defer func() {
				cancel()
				waitInformers()
			}()

			// Add the revision were testing.
			servfake.ServingV1().Revisions(tc.revision.Namespace).Create(tc.revision)
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

			fake.CoreV1().Endpoints(testNamespace).Create(publicEp)
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
			wantCapacity := 1
			cc := tc.revision.Spec.ContainerConcurrency
			dests := tc.initUpdates[len(tc.initUpdates)-1].Dests.Len()
			if *cc != 0 {
				wantCapacity = dests * int(*cc)
			}
			if err := wait.PollImmediate(10*time.Millisecond, 3*time.Second, func() (bool, error) {
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
			t.Logf("This activator idx = %d", rt.activatorIndex)

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

			gotDests := sets.NewString()
			for i := 0; i < tc.requests; i++ {
				result := <-resultChan
				gotDests.Insert(result.dest)
			}

			if got, want := gotDests.List(), tc.wantDests.List(); !cmp.Equal(want, got) {
				t.Errorf("Dests = %v, want: %v, diff: %s", got, want, cmp.Diff(want, got))
				rt.mux.RLock()
				defer rt.mux.RUnlock()
				t.Log("podTrackers:\n", spew.Sdump(rt.podTrackers))
				t.Log("assignedTrackers:\n", spew.Sdump(rt.assignedTrackers))
			}
		})
	}
}

func trackerDestSet(ts []*podTracker) sets.String {
	ret := sets.NewString()
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
	rt := newRevisionThrottler(revName, 42 /*cc*/, networking.ServicePortNameHTTP1, testBreakerParams, logger)
	rt.numActivators.Store(4)
	rt.activatorIndex.Store(0)
	throttler.revisionThrottlers[revName] = rt

	update := revisionDestsUpdate{
		Rev:           revName,
		ClusterIPDest: "",
		Dests:         sets.NewString("ip4", "ip3", "ip5", "ip2", "ip1", "ip0"),
	}
	// This should synchronously update throughout the system.
	// And now we can inspect `rt`.
	throttler.handleUpdate(update)
	if got, want := len(rt.podTrackers), len(update.Dests); got != want {
		t.Errorf("NumTrackers = %d, want: %d", got, want)
	}
	if got, want := trackerDestSet(rt.assignedTrackers), sets.NewString("ip0", "ip4", "ip5"); !got.Equal(want) {
		t.Errorf("Assigned trackers = %v, want: %v, diff: %s", got, want, cmp.Diff(want, got))
	}
	if got, want := rt.breaker.Capacity(), 6*42/4; got != want {
		t.Errorf("TotalCapacity = %d, want: %d", got, want)
	}
	if got, want := rt.assignedTrackers[0].Capacity(), 42; got != want {
		t.Errorf("Exclusive tracker capacity: %d, want: %d", got, want)
	}
	if got, want := rt.assignedTrackers[1].Capacity(), int(math.Ceil(42./4.)); got != want {
		t.Errorf("Shared tracker capacity: %d, want: %d", got, want)
	}
	if got, want := rt.assignedTrackers[2].Capacity(), int(math.Ceil(42./4.)); got != want {
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
	if got, want := rt.breaker.Capacity(), 0; got != want {
		t.Errorf("TotalCapacity = %d, want: %d", got, want)
	}
}

func TestPodAssignmentInfinite(t *testing.T) {
	logger := TestLogger(t)
	revName := types.NamespacedName{Namespace: testNamespace, Name: testRevision}

	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()

	throttler := newTestThrottler(ctx)
	rt := newRevisionThrottler(revName, 0 /*cc*/, networking.ServicePortNameHTTP1, testBreakerParams, logger)
	throttler.revisionThrottlers[revName] = rt

	update := revisionDestsUpdate{
		Rev:           revName,
		ClusterIPDest: "",
		Dests:         sets.NewString("ip3", "ip2", "ip1"),
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
	if got, want := rt.breaker.Capacity(), 1; got != want {
		t.Errorf("TotalCapacity = %d, want: %d", got, want)
	}
	if got, want := rt.assignedTrackers[0].Capacity(), 1; got != want {
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
	if got, want := rt.breaker.Capacity(), 0; got != want {
		t.Errorf("TotalCapacity = %d, want: %d", got, want)
	}
}

func TestActivatorsIndexUpdate(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	fake := fakekubeclient.Get(ctx)
	endpoints := fakeendpointsinformer.Get(ctx)
	servfake := fakeservingclient.Get(ctx)
	revisions := revisioninformer.Get(ctx)

	waitInformers, err := controller.RunInformers(ctx.Done(), endpoints.Informer(), revisions.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	revID := types.NamespacedName{Namespace: testNamespace, Name: testRevision}
	rev := revisionCC1(revID, networking.ProtocolH2C)
	// Add the revision we're testing.
	servfake.ServingV1().Revisions(rev.Namespace).Create(rev)
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

	possibleDests := sets.NewString("128.0.0.1:1234", "128.0.0.2:1234", "128.0.0.23:1234")
	updateCh <- (revisionDestsUpdate{
		Rev:   revID,
		Dests: possibleDests,
	})

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
	fake.CoreV1().Endpoints(testNamespace).Create(publicEp)
	endpoints.Informer().GetIndexer().Add(publicEp)

	rt, err := throttler.getOrCreateRevisionThrottler(revID)
	if err != nil {
		t.Fatal("RevisionThrottler can't be found:", err)
	}

	// Verify capacity gets updated. This is the very last thing we update
	// so we now know that the rest is set statically.
	if err := wait.PollImmediate(10*time.Millisecond, time.Second, func() (bool, error) {
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
	if got, want := len(rt.assignedTrackers), 2; got != want {
		t.Fatalf("len(assignedTrackers) = %d, want %d", got, want)
	}

	publicEp.Subsets = []corev1.EndpointSubset{
		*epSubset(8013, "http2", []string{"130.0.0.2"}, nil),
	}

	fake.CoreV1().Endpoints(testNamespace).Update(publicEp)
	endpoints.Informer().GetIndexer().Update(publicEp)

	// Verify the index was computed.
	if err := wait.PollImmediate(10*time.Millisecond, time.Second, func() (bool, error) {
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

	waitInformers, err := controller.RunInformers(ctx.Done(), endpoints.Informer(), revisions.Informer())
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}

	rev := revisionCC1(types.NamespacedName{Namespace: testNamespace, Name: testRevision}, networking.ProtocolHTTP1)
	// Add the revision we're testing.
	servfake.ServingV1().Revisions(rev.Namespace).Create(rev)
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
	possibleDests := sets.NewString("128.0.0.1:1234", "128.0.0.2:1234", "128.0.0.23:1234")
	updateCh <- (revisionDestsUpdate{
		Rev:   revID,
		Dests: possibleDests,
	})

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
				nil)},
	}
	fake.CoreV1().Endpoints(testNamespace).Create(publicEp)
	endpoints.Informer().GetIndexer().Add(publicEp)

	rt, err := throttler.getOrCreateRevisionThrottler(revID)
	if err != nil {
		t.Fatal("RevisionThrottler can't be found:", err)
	}

	// Verify capacity gets updated. This is the very last thing we update
	// so we now know that we got and processed both the activator endpoints
	// and the application endpoints.
	if err := wait.PollImmediate(10*time.Millisecond, time.Second, func() (bool, error) {
		return rt.breaker.Capacity() == 1, nil
	}); err != nil {
		t.Fatal("Timed out waiting for the capacity to be updated")
	}
	t.Logf("This activator idx = %d", rt.activatorIndex.Load())

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
	if result := <-resultChan; result.err != context.DeadlineExceeded {
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
	tttl := newRevisionThrottler(types.NamespacedName{Namespace: "a", Name: "b"}, 0, /*cc*/
		networking.ServicePortNameHTTP1, queue.BreakerParams{}, TestLogger(t))
	if _, ok := tttl.breaker.(*infiniteBreaker); !ok {
		t.Errorf("The type of revisionBreaker = %T, want %T", tttl, (*infiniteBreaker)(nil))
	}
}

func (t *Throttler) try(ctx context.Context, requests int, try func(string) error) chan tryResult {
	resultChan := make(chan tryResult)

	ctx = util.WithRevID(ctx, types.NamespacedName{Namespace: testNamespace, Name: testRevision})
	for i := 0; i < requests; i++ {
		go func() {
			var result tryResult
			if err := t.Try(ctx, func(dest string) error {
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
	if got, want := b.Capacity(), 0; got != want {
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
	if got, want := b.Capacity(), 1; got != want {
		t.Errorf("Cap=%d, want: %d", got, want)
	}

	// Verify we call the thunk when we have achieved capacity.
	// Twice.
	for i := 0; i < 2; i++ {
		ctx, cancel = context.WithCancel(context.Background())
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
	if got, want := b.Capacity(), 0; got != want {
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
				t.Errorf("Remanants = %d, want: %d", got, want)
			}
		})
	}
}

func TestAssignSlice(t *testing.T) {
	opt := cmp.Comparer(func(a, b *podTracker) bool {
		return a.dest == b.dest
	})
	// assignSlice receives the pod trackers sorted.
	trackers := []*podTracker{{
		dest: "1",
	}, {
		dest: "2",
	}, {
		dest: "3",
	}}
	t.Run("notrackers", func(t *testing.T) {
		got := assignSlice([]*podTracker{}, 0 /*selfIdx*/, 1 /*numAct*/, 42 /*cc*/)
		if !cmp.Equal(got, []*podTracker{}, opt) {
			t.Errorf("Got=%v, want: %v, diff: %s", got, trackers,
				cmp.Diff([]*podTracker{}, got, opt))
		}
	})
	t.Run("idx=1, na=1", func(t *testing.T) {
		got := assignSlice(trackers, 1, 1, 1982)
		if !cmp.Equal(got, trackers, opt) {
			t.Errorf("Got=%v, want: %v, diff: %s", got, trackers,
				cmp.Diff(trackers, got, opt))
		}
	})
	t.Run("idx=-1", func(t *testing.T) {
		got := assignSlice(trackers, -1, 1, 1982)
		if !cmp.Equal(got, trackers, opt) {
			t.Errorf("Got=%v, want: %v, diff: %s", got, trackers,
				cmp.Diff(trackers, got, opt))
		}
	})
	t.Run("idx=1", func(t *testing.T) {
		cp := append(trackers[:0:0], trackers...)
		got := assignSlice(cp, 1, 3, 1984)
		if !cmp.Equal(got, trackers[1:2], opt) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, trackers[0:1],
				cmp.Diff(trackers[1:2], got, opt))
		}
	})
	t.Run("len=1", func(t *testing.T) {
		got := assignSlice(trackers[0:1], 1, 3, 1988)
		if !cmp.Equal(got, trackers[0:1], opt) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, trackers[0:1],
				cmp.Diff(trackers[0:1], got, opt))
		}
	})

	t.Run("idx=1, cc=5", func(t *testing.T) {
		trackers := []*podTracker{{
			dest: "1",
			b:    queue.NewBreaker(testBreakerParams),
		}, {
			dest: "2",
			b:    queue.NewBreaker(testBreakerParams),
		}, {
			dest: "3",
			b:    queue.NewBreaker(testBreakerParams),
		}}
		cp := append(trackers[:0:0], trackers...)
		got := assignSlice(cp, 1, 2, 5)
		want := trackers[1:3]
		if !cmp.Equal(got, want, opt) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, want,
				cmp.Diff(want, got, opt))
		}
		if got, want := got[1].b.Capacity(), 5/2+1; got != want {
			t.Errorf("Capacity for the tail pod = %d, want: %d", got, want)
		}
	})
	t.Run("idx=1, cc=6", func(t *testing.T) {
		trackers := []*podTracker{{
			dest: "1",
			b:    queue.NewBreaker(testBreakerParams),
		}, {
			dest: "2",
			b:    queue.NewBreaker(testBreakerParams),
		}, {
			dest: "3",
			b:    queue.NewBreaker(testBreakerParams),
		}}
		cp := append(trackers[:0:0], trackers...)
		got := assignSlice(cp, 1, 2, 6)
		want := trackers[1:]
		if !cmp.Equal(got, want, opt) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, want,
				cmp.Diff(want, got, opt))
		}
		if got, want := got[1].b.Capacity(), 3; got != want {
			t.Errorf("Capacity for the tail pod = %d, want: %d", got, want)
		}
	})
}
