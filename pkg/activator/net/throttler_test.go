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
	"regexp"
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

type tryResult struct {
	dest      string
	errString string
}

func TestThrottlerUpdateCapacity(t *testing.T) {
	logger := TestLogger(t)
	params := queue.BreakerParams{
		QueueDepth:      1,
		MaxConcurrency:  defaultMaxConcurrency,
		InitialCapacity: 0,
	}
	throttler := &Throttler{
		revisionThrottlers: make(map[types.NamespacedName]*revisionThrottler),
		breakerParams:      params,
		numActivators:      1,
		logger:             logger,
	}
	rt := &revisionThrottler{
		logger:               logger,
		breaker:              queue.NewBreaker(params),
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
	rt.podIPTrackers = makeTrackers(1)
	rt.containerConcurrency = 10
	rt.updateCapacity(throttler, 0 /* doesn't matter here*/)
	if got, want := rt.breaker.Capacity(), 10; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// 2 backends.
	rt.podIPTrackers = makeTrackers(2)
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
	rt.podIPTrackers = makeTrackers(3)
	rt.updateCapacity(throttler, 2)
	if got, want := rt.breaker.Capacity(), 15; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}

	// 3 pods, index 1.
	throttler.activatorIndex = 1
	rt.updateCapacity(throttler, 1)
	if got, want := rt.breaker.Capacity(), 15; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
}

func makeTrackers(num int) []*podIPTracker {
	x := make([]*podIPTracker, num)
	for i := 0; i < num; i++ {
		x[i] = &podIPTracker{dest: strconv.Itoa(i)}
	}
	return x
}

func TestThrottlerWithError(t *testing.T) {
	for _, tc := range []struct {
		name        string
		revision    *v1alpha1.Revision
		initUpdate  revisionDestsUpdate
		delete      *types.NamespacedName
		trys        []types.NamespacedName
		wantResults []tryResult
	}{{
		name:     "second request timeout",
		revision: revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		initUpdate: revisionDestsUpdate{
			Rev:           types.NamespacedName{testNamespace, testRevision},
			ClusterIPDest: "129.0.0.1:1234",
			Dests:         sets.NewString("128.0.0.1:1234"),
		},
		trys: []types.NamespacedName{
			{Namespace: testNamespace, Name: testRevision},
			{Namespace: testNamespace, Name: testRevision},
		},
		wantResults: []tryResult{
			{dest: "129.0.0.1:1234"},
			{errString: context.DeadlineExceeded.Error()},
		},
	}, {
		name:     "remove before try",
		revision: revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		initUpdate: revisionDestsUpdate{
			Rev:   types.NamespacedName{testNamespace, testRevision},
			Dests: sets.NewString("128.0.0.1:1234"),
		},
		delete: &types.NamespacedName{
			testNamespace, testRevision,
		},
		trys: []types.NamespacedName{
			{Namespace: testNamespace, Name: testRevision},
		},
		wantResults: []tryResult{
			{errString: "revision.serving.knative.dev \"test-revision\" not found"},
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer cancel()
			updateCh := make(chan revisionDestsUpdate, 2)

			params := queue.BreakerParams{
				QueueDepth:      1,
				MaxConcurrency:  defaultMaxConcurrency,
				InitialCapacity: 0,
			}

			endpoints := fakeendpointsinformer.Get(ctx)

			servfake := fakeservingclient.Get(ctx)
			revisions := fakerevisioninformer.Get(ctx)
			controller.StartInformers(ctx.Done(), endpoints.Informer(), revisions.Informer())

			// Add the revision we're testing
			servfake.ServingV1alpha1().Revisions(tc.revision.Namespace).Create(tc.revision)
			revisions.Informer().GetIndexer().Add(tc.revision)

			throttler := NewThrottler(ctx, params, "10.10.10.10:8012")
			updateCh <- tc.initUpdate
			close(updateCh)

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				throttler.run(updateCh)
			}()

			// Wait for throttler to complete processing updates and exit
			wg.Wait()

			// Make sure our informer event has fired.
			if tc.delete != nil {
				servfake.ServingV1alpha1().Revisions(tc.delete.Namespace).Delete(tc.delete.Name, nil)
				revisions.Informer().GetIndexer().Delete(tc.delete)
				time.Sleep(200 * time.Millisecond)
			}

			tryContext, cancel := context.WithTimeout(context.TODO(), 100*time.Millisecond)
			defer cancel()

			gotTries := tryThrottler(throttler, tc.trys, tryContext)

			if got, want := gotTries, tc.wantResults; !cmp.Equal(got, want, cmp.AllowUnexported(tryResult{})) {
				t.Errorf("Dests = %v, want: %v, diff: %s", got, want, cmp.Diff(want, got, cmp.AllowUnexported(tryResult{})))
			}
		})
	}
}

func TestThrottlerSuccesses(t *testing.T) {
	for _, tc := range []struct {
		name        string
		revision    *v1alpha1.Revision
		initUpdates []revisionDestsUpdate
		trys        []types.NamespacedName
		wantDests   sets.String
	}{{
		name:     "single healthy podIP",
		revision: revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{testNamespace, testRevision},
			Dests: sets.NewString("128.0.0.1:1234"),
		}, {
			Rev:   types.NamespacedName{testNamespace, testRevision},
			Dests: sets.NewString("128.0.0.1:1234"),
		}},
		trys: []types.NamespacedName{
			{Namespace: testNamespace, Name: testRevision},
		},
		wantDests: sets.NewString("128.0.0.1:1234"),
	}, {
		name:     "single healthy clusterIP",
		revision: revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{testNamespace, testRevision},
			Dests: sets.NewString("128.0.0.1:1234", "128.0.0.2:1234"),
		}, {
			Rev:           types.NamespacedName{testNamespace, testRevision},
			ClusterIPDest: "129.0.0.1:1234",
			Dests:         sets.NewString("128.0.0.1:1234"),
		}},
		trys: []types.NamespacedName{
			{Namespace: testNamespace, Name: testRevision},
		},
		wantDests: sets.NewString("129.0.0.1:1234"),
	}, {
		name:     "spread podIP load",
		revision: revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			Rev:   types.NamespacedName{testNamespace, testRevision},
			Dests: sets.NewString("128.0.0.1:1234", "128.0.0.2:1234"),
		}},
		trys: []types.NamespacedName{
			{Namespace: testNamespace, Name: testRevision},
			{Namespace: testNamespace, Name: testRevision},
		},
		wantDests: sets.NewString("128.0.0.2:1234", "128.0.0.1:1234"),
	}, {
		name:     "multiple ClusterIP requests",
		revision: revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1),
		initUpdates: []revisionDestsUpdate{{
			Rev:           types.NamespacedName{testNamespace, testRevision},
			ClusterIPDest: "129.0.0.1:1234",
			Dests:         sets.NewString("128.0.0.1:1234", "128.0.0.2:1234"),
		}},
		trys: []types.NamespacedName{
			{Namespace: testNamespace, Name: testRevision},
			{Namespace: testNamespace, Name: testRevision},
		},
		wantDests: sets.NewString("129.0.0.1:1234"),
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
			defer cancel()
			updateCh := make(chan revisionDestsUpdate, 2)

			params := queue.BreakerParams{
				QueueDepth:      1,
				MaxConcurrency:  defaultMaxConcurrency,
				InitialCapacity: 0,
			}

			endpoints := fakeendpointsinformer.Get(ctx)
			servfake := fakeservingclient.Get(ctx)
			revisions := fakerevisioninformer.Get(ctx)

			controller.StartInformers(ctx.Done(), endpoints.Informer(), revisions.Informer())

			// Add the revision were testing
			servfake.ServingV1alpha1().Revisions(tc.revision.Namespace).Create(tc.revision)
			revisions.Informer().GetIndexer().Add(tc.revision)

			throttler := NewThrottler(ctx, params, "10.10.10.10:8012")
			atomic.StoreInt32(&throttler.numActivators, 1)
			atomic.StoreInt32(&throttler.activatorIndex, 0)

			for _, update := range tc.initUpdates {
				updateCh <- update
			}
			close(updateCh)

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				throttler.run(updateCh)
			}()

			// Wait for throttler to complete processing updates and exit
			wg.Wait()

			tryContext, cancel := context.WithTimeout(context.TODO(), 100*time.Millisecond)
			defer cancel()

			gotTries := tryThrottler(throttler, tc.trys, tryContext)
			gotDests := sets.NewString()
			for _, tr := range gotTries {
				gotDests.Insert(tr.dest)
			}

			if got, want := gotDests, tc.wantDests; !got.Equal(want) {
				t.Errorf("Dests = %v, want: %v, diff: %s", got, want, cmp.Diff(want, got))
			}
		})
	}
}

func TestMultipleActivators(t *testing.T) {
	ctx, cancel, _ := rtesting.SetupFakeContextWithCancel(t)
	defer cancel()

	fake := fakekubeclient.Get(ctx)
	endpoints := fakeendpointsinformer.Get(ctx)
	servfake := fakeservingclient.Get(ctx)
	revisions := revisioninformer.Get(ctx)

	controller.StartInformers(ctx.Done(), endpoints.Informer(), revisions.Informer())

	rev := revision(types.NamespacedName{testNamespace, testRevision}, networking.ProtocolHTTP1)
	// Add the revision were testing
	servfake.ServingV1alpha1().Revisions(rev.Namespace).Create(rev)
	revisions.Informer().GetIndexer().Add(rev)

	params := queue.BreakerParams{
		QueueDepth:      1,
		MaxConcurrency:  defaultMaxConcurrency,
		InitialCapacity: 0,
	}

	throttler := NewThrottler(ctx, params, "130.0.0.2:8012")

	revID := types.NamespacedName{testNamespace, testRevision}
	possibleDests := sets.NewString("128.0.0.1:1234", "128.0.0.2:1234", "128.0.0.23:1234")
	updateCh := make(chan revisionDestsUpdate, 1)
	updateCh <- revisionDestsUpdate{
		Rev:   revID,
		Dests: possibleDests,
	}
	close(updateCh)
	throttler.run(updateCh)

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
	tryContext, cancel2 := context.WithTimeout(context.TODO(), 100*time.Millisecond)
	defer cancel2()

	results := tryThrottler(throttler, []types.NamespacedName{revID, revID}, tryContext)
	if !possibleDests.Has(results[0].dest) {
		t.Errorf("Request went to an unknown destination: %s, possibles: %v", results[0].dest, possibleDests)
	}
	if got, want := results[1].errString, context.DeadlineExceeded.Error(); got != want {
		t.Errorf("Error = %s, want: %s", got, want)
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

func tryThrottler(throttler *Throttler, trys []types.NamespacedName, ctx context.Context) []tryResult {
	resCh := make(chan tryResult)
	defer close(resCh)
	var tryWaitg sync.WaitGroup
	tryWaitg.Add(len(trys))
	for _, revID := range trys {
		go func(revID types.NamespacedName) {
			err := throttler.Try(ctx, revID, func(dest string) error {
				tryWaitg.Done()
				resCh <- tryResult{dest: dest}
				return nil
			})
			if err != nil {
				tryWaitg.Done()
				resCh <- tryResult{errString: err.Error()}
			}
		}(revID)
	}

	tryWaitg.Wait()

	ret := make([]tryResult, len(trys))
	for i := range trys {
		ret[i] = <-resCh
	}
	return ret
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

func TestPickP2C(t *testing.T) {
	tests := []struct {
		l      string
		tgts   []*podIPTracker
		wantRE string
	}{{
		l:      "empty",
		tgts:   []*podIPTracker{},
		wantRE: "",
	}, {
		l: "single",
		tgts: []*podIPTracker{
			{
				dest:     "good-place",
				requests: 1,
				weight:   1,
			},
		},
		wantRE: "good-place",
	}, {
		l: "two",
		tgts: []*podIPTracker{
			{
				dest:     "bad-place",
				requests: 11,
				weight:   1,
			},
			{
				dest:     "good-place",
				requests: 1,
				weight:   1,
			},
		},
		wantRE: "good-place",
	}, {
		l: "three",
		tgts: []*podIPTracker{
			{
				dest:     "bad",
				requests: 7,
				weight:   1,
			},
			{
				dest:     "neutral",
				requests: 5,
				weight:   1,
			},
			{
				dest:     "good",
				requests: 1,
				weight:   1,
			},
		},
		wantRE: "good|neutral",
	}}
	for _, test := range tests {
		t.Run(test.l, func(t *testing.T) {
			// Run the same code several times to make sure the selection is stable and callback
			// properly removes the load.
			for i := 0; i < 5; i++ {
				re := regexp.MustCompile(test.wantRE)
				got, cb := pickP2C(test.tgts)
				if !re.MatchString(got) {
					t.Errorf("target = %s, want to match: %s", got, test.wantRE)
				}
				if cb != nil {
					cb()
				}
			}
		})
	}
	t.Run("multiple", func(t *testing.T) {
		tgts := []*podIPTracker{
			{
				dest:     "bad-place",
				requests: 3,
				weight:   1,
			},
			{
				dest:     "good-place",
				requests: 1,
				weight:   1,
			},
		}
		cbs := make([]func(), 0, 4)
		for i := 0; i < 2; i++ {
			got, cb := pickP2C(tgts)
			if got != "good-place" {
				t.Fatalf("Got = %s, want: good-place", got)
			}
			cbs = append(cbs, cb)
		}
		// Now this next request can be `good` or `bad` depending on random # generator
		got, cb := pickP2C(tgts)
		cbs = append(cbs, cb)
		if got == "good-place" {
			got, cb = pickP2C(tgts)
			cbs = append(cbs, cb)
			if got != "bad-place" {
				t.Errorf("Got = %s, want: bad-place", got)
			}
		}
		for _, cb := range cbs {
			cb()
		}
	})
}

func TestPickIndices(t *testing.T) {
	tests := []struct {
		l                      string
		pods                   int
		acts                   int
		idx                    int
		wantB, wantE, wantTail int
	}{{
		l:        "1 pod, 1 activator",
		pods:     1,
		acts:     1,
		idx:      0,
		wantB:    0,
		wantE:    1,
		wantTail: 0,
	}, {
		l:        "1 pod, 2 activators, this is 0",
		pods:     1,
		acts:     2,
		idx:      0,
		wantB:    0,
		wantE:    1,
		wantTail: 0,
	}, {
		l:        "1 pod, 2 activators, this is 1",
		pods:     1,
		acts:     2,
		idx:      1,
		wantB:    0,
		wantE:    1,
		wantTail: 0,
	}, {
		l:     "2 pods, 3 activators, this is 1",
		pods:  2,
		acts:  3,
		idx:   1,
		wantB: 1,
		wantE: 2,
	}, {
		l:        "2 pods, 3 activators, this is 2",
		pods:     2,
		acts:     3,
		idx:      2,
		wantB:    0,
		wantE:    1,
		wantTail: 0,
	}, {
		l:        "3 pods, 3 activators, this is 2",
		pods:     3,
		acts:     3,
		idx:      2,
		wantB:    2,
		wantE:    3,
		wantTail: 0,
	}, {
		l:        "10 pods, 3 activators this is 0",
		pods:     10,
		acts:     3,
		idx:      0,
		wantB:    0,
		wantE:    3,
		wantTail: 1,
	}, {
		l:        "10 pods, 3 activators this is 1",
		pods:     10,
		acts:     3,
		idx:      1,
		wantB:    3,
		wantE:    6,
		wantTail: 1,
	}, {
		l:        "10 pods, 3 activators this is 2",
		pods:     10,
		acts:     3,
		idx:      2,
		wantB:    6,
		wantE:    9,
		wantTail: 1,
	}, {
		l:        "150 pods, 5 activators this is 0",
		pods:     150,
		acts:     5,
		idx:      0,
		wantB:    0,
		wantE:    30,
		wantTail: 0,
	}, {
		l:        "150 pods, 5 activators this is 1",
		pods:     150,
		acts:     5,
		idx:      1,
		wantB:    30,
		wantE:    60,
		wantTail: 0,
	}, {
		l:        "150 pods, 3 activators this is 4",
		pods:     150,
		acts:     5,
		idx:      4,
		wantB:    120,
		wantE:    150,
		wantTail: 0,
	}, {
		l:        "16 pods, 7 activators, this is activator 5",
		pods:     16,
		acts:     7,
		idx:      5,
		wantB:    10,
		wantE:    12,
		wantTail: 2,
	}}
	for _, test := range tests {
		t.Run(test.l, func(tt *testing.T) {
			bi, ei, tail := pickIndices(test.pods, test.idx, test.acts)
			if got, want := bi, test.wantB; got != want {
				t.Errorf("BeginIndex = %d, want: %d", got, want)
			}
			if got, want := ei, test.wantE; got != want {
				t.Errorf("EndIndex = %d, want: %d", got, want)
			}
			if got, want := tail, test.wantTail; got != want {
				t.Errorf("Tail = %d, want: %d", got, want)
			}
		})
	}
}

func TestAssignSlice(t *testing.T) {
	trackers := []*podIPTracker{
		{
			dest: "2",
		},
		{
			dest: "1",
		},
		{
			dest: "3",
		},
	}
	t.Run("notrackers", func(t *testing.T) {
		got := assignSlice([]*podIPTracker{}, 0, 1)
		if !cmp.Equal(got, []*podIPTracker{}, cmpopts.IgnoreUnexported(podIPTracker{})) {
			t.Errorf("Got=%v, want: %v, diff: %s", got, trackers,
				cmp.Diff([]*podIPTracker{}, got, cmpopts.IgnoreUnexported(podIPTracker{})))
		}
	})
	t.Run("idx=-1", func(t *testing.T) {
		got := assignSlice(trackers, -1, 1)
		if !cmp.Equal(got, trackers, cmpopts.IgnoreUnexported(podIPTracker{})) {
			t.Errorf("Got=%v, want: %v, diff: %s", got, trackers,
				cmp.Diff(trackers, got, cmpopts.IgnoreUnexported(podIPTracker{})))
		}
	})
	t.Run("idx=1", func(t *testing.T) {
		cp := append(trackers[:0:0], trackers...)
		got := assignSlice(cp, 1, 3)
		if !cmp.Equal(got, trackers[0:1], cmpopts.IgnoreUnexported(podIPTracker{})) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, trackers[0:1],
				cmp.Diff(trackers[0:1], got, cmpopts.IgnoreUnexported(podIPTracker{})))
		}
	})
	t.Run("len=1", func(t *testing.T) {
		got := assignSlice(trackers[0:1], 1, 3)
		if !cmp.Equal(got, trackers[0:1], cmpopts.IgnoreUnexported(podIPTracker{})) {
			t.Errorf("Got=%v, want: %v; diff: %s", got, trackers[0:1],
				cmp.Diff(trackers[0:1], got, cmpopts.IgnoreUnexported(podIPTracker{})))
		}
	})
}
