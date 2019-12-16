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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeendpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints/fake"
	"knative.dev/pkg/controller"
	. "knative.dev/pkg/logging/testing"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision/fake"
	"knative.dev/serving/pkg/queue"
)

const defaultMaxConcurrency = 1000

var defaultParams = queue.BreakerParams{
	QueueDepth:      1,
	MaxConcurrency:  defaultMaxConcurrency,
	InitialCapacity: 0,
}

type tryResult struct {
	dest string
	err  error
}

func newTestThrottler(ctx context.Context, numA int32) *Throttler {
	throttler := NewThrottler(ctx, defaultParams, "10.10.10.10:8012")
	atomic.StoreInt32(&throttler.numActivators, numA)
	atomic.StoreInt32(&throttler.activatorIndex, 0)
	return throttler
}

func TestThrottlerUpdateCapacity(t *testing.T) {
	logger := TestLogger(t)
	throttler := &Throttler{
		revisionThrottlers: make(map[types.NamespacedName]*revisionThrottler),
		breakerParams:      defaultParams,
		numActivators:      1,
		logger:             logger,
	}
	rt := &revisionThrottler{
		logger:               logger,
		breaker:              queue.NewBreaker(defaultParams),
		containerConcurrency: 10,
	}

	rt.updateCapacity(throttler, 1)
	if got, want := rt.breaker.Capacity(), 10; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	rt.updateCapacity(throttler, 10)
	if got, want := rt.breaker.Capacity(), 100; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	rt.updateCapacity(throttler, defaultMaxConcurrency) // So in theory should be 10x.
	if got, want := rt.breaker.Capacity(), defaultMaxConcurrency; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	throttler.numActivators = 10
	rt.updateCapacity(throttler, 10)
	if got, want := rt.breaker.Capacity(), 10; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	throttler.numActivators = 200
	rt.updateCapacity(throttler, 10)
	if got, want := rt.breaker.Capacity(), 1; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	rt.updateCapacity(throttler, 0)
	if got, want := rt.breaker.Capacity(), 0; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	rt.containerConcurrency = 0
	rt.updateCapacity(throttler, 1)
	if got, want := rt.breaker.Capacity(), defaultMaxConcurrency; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	rt.updateCapacity(throttler, 10)
	if got, want := rt.breaker.Capacity(), defaultMaxConcurrency; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	throttler.numActivators = 200
	rt.updateCapacity(throttler, 1)
	if got, want := rt.breaker.Capacity(), defaultMaxConcurrency; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	rt.updateCapacity(throttler, 0)
	if got, want := rt.breaker.Capacity(), 0; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// Now test with podIP trackers in tow.
	// Simple case.
	throttler.numActivators = 1
	throttler.activatorIndex = 0
	rt.podTrackers = makeTrackers(1, 10)
	rt.containerConcurrency = 10
	rt.updateCapacity(throttler, 0 /* doesn't matter here*/)
	if got, want := rt.breaker.Capacity(), 10; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// 2 backends.
	rt.podTrackers = makeTrackers(2, 10)
	rt.updateCapacity(throttler, -1 /* doesn't matter here*/)
	if got, want := rt.breaker.Capacity(), 20; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// 2 activators.
	throttler.numActivators = 2
	rt.updateCapacity(throttler, -1 /* doesn't matter here*/)
	if got, want := rt.breaker.Capacity(), 10; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// 3 pods, index 0.
	rt.podTrackers = makeTrackers(3, 10)
	rt.updateCapacity(throttler, -1 /* doesn't matter here*/)
	if got, want := rt.breaker.Capacity(), 15; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// 3 pods, index 1.
	throttler.activatorIndex = 1
	rt.updateCapacity(throttler, -1 /* doesn't matter here*/)
	if got, want := rt.breaker.Capacity(), 15; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// Inifinite capacity.
	throttler.activatorIndex = 1
	rt.containerConcurrency = 0
	rt.podTrackers = makeTrackers(3, 0)
	rt.updateCapacity(throttler, 1)
	if got, want := rt.breaker.Capacity(), defaultMaxConcurrency; got != want {
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
		t.Fatalf("Failed to start informers: %v", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	// Add the revision we're testing.
	revID := types.NamespacedName{testNamespace, testRevision}
	revision := revisionCC1(revID, networking.ProtocolHTTP1)
	servfake.ServingV1alpha1().Revisions(revision.Namespace).Create(revision)
	revisions.Informer().GetIndexer().Add(revision)

	throttler := newTestThrottler(ctx, 1)
	throttler.handleUpdate(revisionDestsUpdate{
		Rev:   revID,
		Dests: sets.NewString("128.0.0.1:1234"),
	})

	// Make sure it now works.
	if err := throttler.Try(context.Background(), revID, func(string) error { return nil }); err != nil {
		t.Fatalf("Try() = %v, want no error", err)
	}

	// Make sure errors are propagated correctly.
	innerError := errors.New("inner")
	if err := throttler.Try(context.Background(), revID, func(string) error { return innerError }); err != innerError {
		t.Fatalf("Try() = %v, want %v", err, innerError)
	}

	servfake.ServingV1alpha1().Revisions(revision.Namespace).Delete(revision.Name, nil)
	revisions.Informer().GetIndexer().Delete(revID)

	// Eventually it should now fail.
	var lastError error
	wait.PollInfinite(10*time.Millisecond, func() (bool, error) {
		lastError = throttler.Try(context.Background(), revID, func(string) error { return nil })
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
		t.Fatalf("Failed to start informers: %v", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	// Add the revision we're testing.
	revID := types.NamespacedName{testNamespace, testRevision}
	revision := revisionCC1(revID, networking.ProtocolHTTP1)
	servfake.ServingV1alpha1().Revisions(revision.Namespace).Create(revision)
	revisions.Informer().GetIndexer().Add(revision)

	throttler := newTestThrottler(ctx, 1)
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
	resultChan := tryThrottler(throttler, reqCtx, 2 /*requests*/, func(string) error {
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

func TestThrottlerSuccesses(t *testing.T) {
	for _, tc := range []struct {
		name        string
		revision    *v1alpha1.Revision
		initUpdates []revisionDestsUpdate
		requests    int
		wantDests   sets.String
	}{{
		name:     "single healthy podIP",
		revision: revisionCC1(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{testNamespace, testRevision},
			Dests: sets.NewString("128.0.0.1:1234"),
		}, {
			Rev:   types.NamespacedName{testNamespace, testRevision},
			Dests: sets.NewString("128.0.0.1:1234"),
		}},
		requests:  1,
		wantDests: sets.NewString("128.0.0.1:1234"),
	}, {
		name:     "single healthy podIP, infinite cc",
		revision: revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1, 0),
		// Double updates exercise additional paths.
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{testNamespace, testRevision},
			Dests: sets.NewString("128.0.0.2:1234"),
		}, {
			Rev:   types.NamespacedName{testNamespace, testRevision},
			Dests: sets.NewString("128.0.0.1:1234"),
		}},
		requests:  1,
		wantDests: sets.NewString("128.0.0.1:1234"),
	}, {
		name:     "single healthy clusterIP",
		revision: revisionCC1(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{testNamespace, testRevision},
			Dests: sets.NewString("128.0.0.1:1234", "128.0.0.2:1234"),
		}, {
			Rev:           types.NamespacedName{testNamespace, testRevision},
			ClusterIPDest: "129.0.0.1:1234",
			Dests:         sets.NewString("128.0.0.1:1234"),
		}},
		requests:  1,
		wantDests: sets.NewString("129.0.0.1:1234"),
	}, {
		name:     "spread podIP load",
		revision: revisionCC1(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			// Double update here excercises some additional paths.
			Rev:   types.NamespacedName{testNamespace, testRevision},
			Dests: sets.NewString("128.0.0.3:1234"),
		}, {
			Rev:   types.NamespacedName{testNamespace, testRevision},
			Dests: sets.NewString("128.0.0.1:1234", "128.0.0.2:1234"),
		}},
		requests:  2,
		wantDests: sets.NewString("128.0.0.2:1234", "128.0.0.1:1234"),
	}, {
		name:     "clumping test",
		revision: revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1, 3),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{testNamespace, testRevision},
			Dests: sets.NewString("128.0.0.1:1234", "128.0.0.2:1234"),
		}},
		requests:  3,
		wantDests: sets.NewString("128.0.0.1:1234"),
	}, {
		name:     "multiple ClusterIP requests",
		revision: revisionCC1(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			Rev:           types.NamespacedName{testNamespace, testRevision},
			ClusterIPDest: "129.0.0.1:1234",
			Dests:         sets.NewString("128.0.0.1:1234", "128.0.0.2:1234"),
		}},
		requests:  2,
		wantDests: sets.NewString("129.0.0.1:1234"),
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			servfake := fakeservingclient.Get(ctx)
			revisions := fakerevisioninformer.Get(ctx)

			waitInformers, err := controller.RunInformers(ctx.Done(), revisions.Informer())
			if err != nil {
				t.Fatalf("Failed to start informers: %v", err)
			}
			defer func() {
				cancel()
				waitInformers()
			}()

			// Add the revision were testing.
			servfake.ServingV1alpha1().Revisions(tc.revision.Namespace).Create(tc.revision)
			revisions.Informer().GetIndexer().Add(tc.revision)

			throttler := newTestThrottler(ctx, 1)
			for _, update := range tc.initUpdates {
				throttler.handleUpdate(update)
			}

			tryContext, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel2()

			results := tryThrottler(throttler, tryContext, tc.requests, func(string) error {
				// Simulate proxying.
				time.Sleep(50 * time.Millisecond)
				return nil
			})
			gotDests := sets.NewString()
			for i := 0; i < tc.requests; i++ {
				result := <-results
				gotDests.Insert(result.dest)
			}

			if got, want := gotDests, tc.wantDests; !got.Equal(want) {
				t.Errorf("Dests = %v, want: %v, diff: %s", got, want, cmp.Diff(want, got))
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
	revName := types.NamespacedName{testNamespace, testRevision}

	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()

	throttler := newTestThrottler(ctx, 4 /*num activators*/)
	rt := newRevisionThrottler(revName, 42 /*cc*/, defaultParams, logger)
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
	revName := types.NamespacedName{testNamespace, testRevision}

	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()

	throttler := newTestThrottler(ctx, 2)
	rt := newRevisionThrottler(revName, 0 /*cc*/, defaultParams, logger)
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

func TestMultipleActivators(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)

	fake := fakekubeclient.Get(ctx)
	endpoints := fakeendpointsinformer.Get(ctx)
	servfake := fakeservingclient.Get(ctx)
	revisions := revisioninformer.Get(ctx)

	waitInformers, err := controller.RunInformers(ctx.Done(), endpoints.Informer(), revisions.Informer())
	if err != nil {
		t.Fatalf("Failed to start informers: %v", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	rev := revisionCC1(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1)
	// Add the revision we're testing.
	servfake.ServingV1alpha1().Revisions(rev.Namespace).Create(rev)
	revisions.Informer().GetIndexer().Add(rev)

	throttler := NewThrottler(ctx, defaultParams, "130.0.0.2:8012")

	revID := types.NamespacedName{testNamespace, testRevision}
	possibleDests := sets.NewString("128.0.0.1:1234", "128.0.0.2:1234", "128.0.0.23:1234")
	throttler.handleUpdate(revisionDestsUpdate{
		Rev:   revID,
		Dests: possibleDests,
	})

	// Add activator endpoint with 2 activators.
	activatorEp := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      networking.ActivatorServiceName,
			Namespace: system.Namespace(),
		},
		Subsets: []corev1.EndpointSubset{*epSubset(8012, "http", []string{"130.0.0.1", "130.0.0.2"})},
	}
	fake.CoreV1().Endpoints(system.Namespace()).Create(activatorEp)
	endpoints.Informer().GetIndexer().Add(activatorEp)

	// Make sure our informer event has fired.
	if err := wait.PollImmediate(10*time.Millisecond, 1*time.Second, func() (bool, error) {
		return atomic.LoadInt32(&throttler.activatorIndex) != -1, nil
	}); err != nil {
		t.Fatal("Timed out waiting for the Activator Endpoints to fire")
	}

	// Test with 2 activators, 3 endpoints we can send 1 request and the second times out.
	var mux sync.Mutex
	mux.Lock() // Lock the mutex so all requests are blocked in the Try function.

	reqCtx, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	resultChan := tryThrottler(throttler, reqCtx, 2 /*requests*/, func(string) error {
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
	tttl := newRevisionThrottler(types.NamespacedName{"a", "b"}, 0, /*cc*/
		queue.BreakerParams{}, TestLogger(t))
	if _, ok := tttl.breaker.(*infiniteBreaker); !ok {
		t.Errorf("The type of revisionBreker = %T, want %T", tttl, (*infiniteBreaker)(nil))
	}
}

func tryThrottler(throttler *Throttler, ctx context.Context, requests int, try func(string) error) chan tryResult {
	resultChan := make(chan tryResult)

	for i := 0; i < requests; i++ {
		go func() {
			var result tryResult
			if err := throttler.Try(ctx, types.NamespacedName{Namespace: testNamespace, Name: testRevision}, func(dest string) error {
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
	const myIP = "10.10.10.3:1234"
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
		[]string{"11.11.11.11:1234", "11.11.11.12:1234"},
		-1,
	}, {
		"first",
		[]string{"10.10.10.3:1234,11.11.11.11:1234"},
		-1,
	}, {
		"middle",
		[]string{"10.10.10.1:1212", "10.10.10.2:1234", "10.10.10.3:1234", "11.11.11.11:1234"},
		2,
	}, {
		"last",
		[]string{"10.10.10.1:1234", "10.10.10.2:1234", "10.10.10.3:1234"},
		2,
	}}
	for _, test := range tests {
		t.Run(test.label, func(t *testing.T) {
			if got, want := inferIndex(test.ips, myIP), test.want; got != want {
				t.Errorf("Index = %d, wand: %d", got, want)
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
	opts := []cmp.Option{
		cmpopts.IgnoreUnexported(queue.Breaker{}),
		cmp.AllowUnexported(podTracker{}),
	}
	trackers := []*podTracker{{
		dest: "2",
	}, {
		dest: "1",
	}, {
		dest: "3",
	}}
	t.Run("notrackers", func(t *testing.T) {
		got := assignSlice([]*podTracker{}, 0, 1, 0)
		if !cmp.Equal(got, []*podTracker{}, opts...) {
			t.Errorf("Got=%v, want: %v, diff: %s", got, trackers,
				cmp.Diff([]*podTracker{}, got, opts...))
		}
	})
	t.Run("idx=-1", func(t *testing.T) {
		got := assignSlice(trackers, -1, 1, 0)
		if !cmp.Equal(got, trackers, opts...) {
			t.Errorf("Got=%v, want: %v, diff: %s", got, trackers,
				cmp.Diff(trackers, got, opts...))
		}
	})
	t.Run("idx=1", func(t *testing.T) {
		cp := append(trackers[:0:0], trackers...)
		got := assignSlice(cp, 1, 3, 0)
		if !cmp.Equal(got, trackers[0:1], opts...) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, trackers[0:1],
				cmp.Diff(trackers[0:1], got, opts...))
		}
	})
	t.Run("len=1", func(t *testing.T) {
		got := assignSlice(trackers[0:1], 1, 3, 0)
		if !cmp.Equal(got, trackers[0:1], opts...) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, trackers[0:1],
				cmp.Diff(trackers[0:1], got, opts...))
		}
	})

	t.Run("idx=1, cc=5", func(t *testing.T) {
		trackers := []*podTracker{{
			dest: "2",
			b:    queue.NewBreaker(defaultParams),
		}, {
			dest: "1",
			b:    queue.NewBreaker(defaultParams),
		}, {
			dest: "3",
			b:    queue.NewBreaker(defaultParams),
		}}
		cp := append(trackers[:0:0], trackers...)
		got := assignSlice(cp, 1, 2, 5)
		want := append(trackers[0:1], trackers[2:]...)
		if !cmp.Equal(got, want, opts...) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, want,
				cmp.Diff(trackers[0:1], got, opts...))
		}
		if got, want := got[1].b.Capacity(), 5/2+1; got != want {
			t.Errorf("Capacity for the tail pod = %d, want: %d", got, want)
		}
	})
	t.Run("idx=1, cc=6", func(t *testing.T) {
		trackers := []*podTracker{{
			dest: "2",
			b:    queue.NewBreaker(defaultParams),
		}, {
			dest: "1",
			b:    queue.NewBreaker(defaultParams),
		}, {
			dest: "3",
			b:    queue.NewBreaker(defaultParams),
		}}
		cp := append(trackers[:0:0], trackers...)
		got := assignSlice(cp, 1, 2, 6)
		want := append(trackers[0:1], trackers[2:]...)
		if !cmp.Equal(got, want, opts...) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, want,
				cmp.Diff(trackers[0:1], got, opts...))
		}
		if got, want := got[1].b.Capacity(), 3; got != want {
			t.Errorf("Capacity for the tail pod = %d, want: %d", got, want)
		}
	})
}
