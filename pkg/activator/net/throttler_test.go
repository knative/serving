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

package activator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

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
	defer ClearAll()
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
			defer func() {
				cancel()
				ClearAll()
			}()
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

			throttler := NewThrottler(ctx, params)
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
			defer func() {
				cancel()
				ClearAll()
			}()
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

			throttler := NewThrottler(ctx, params)
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
	defer func() {
		cancel()
		ClearAll()
	}()

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

	throttler := NewThrottler(ctx, params)

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
		Subsets: []corev1.EndpointSubset{*epSubset(999, "http", []string{"130.0.0.1", "130.0.0.2"})},
	}
	fake.CoreV1().Endpoints(system.Namespace()).Create(activatorEp)
	endpoints.Informer().GetIndexer().Add(activatorEp)

	// Make sure our informer event has fired.
	time.Sleep(200 * time.Millisecond)

	// Test with 2 activators, 3 endpoints we can send 1 request.
	func() {
		var cancel context.CancelFunc
		tryContext, cancel := context.WithTimeout(context.TODO(), 100*time.Millisecond)
		defer cancel()

		results := tryThrottler(throttler, []types.NamespacedName{revID, revID}, tryContext)
		if !possibleDests.Has(results[0].dest) {
			t.Errorf("Request went to an unknown destination: %s, possibles: %v", results[0].dest, possibleDests)
		}
		if got, want := results[1].errString, context.DeadlineExceeded.Error(); got != want {
			t.Errorf("Error = %s, want: %s", got, want)
		}
	}()
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
	defer ClearAll()
	b := &InfiniteBreaker{
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
