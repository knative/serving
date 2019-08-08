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
	"math/rand"
	"strconv"
	"testing"
	"time"

	"go.uber.org/zap"

	"knative.dev/pkg/controller"
	. "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/system"
	"knative.dev/pkg/test/helpers"
	"knative.dev/serving/pkg/apis/networking"
	nv1a1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	servingfake "knative.dev/serving/pkg/client/clientset/versioned/fake"
	servinginformers "knative.dev/serving/pkg/client/informers/externalversions"
	netlisters "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	"knative.dev/serving/pkg/queue"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"knative.dev/pkg/ptr"
	_ "knative.dev/pkg/system/testing"
)

const (
	testNamespace               = "good-namespace"
	testRevision                = "good-name"
	defaultMaxConcurrency       = 1000
	defaultConcurrency    int64 = 10
	initCapacity                = 0
)

var revID = RevisionID{testNamespace, testRevision}

func TestThrottlerUpdateCapacity(t *testing.T) {
	samples := []struct {
		label          string
		revisionLister servinglisters.RevisionLister
		numEndpoints   int
		maxConcurrency int
		want           int
		wantError      bool
	}{{
		label:          "all good",
		revisionLister: revisionLister(testNamespace, testRevision, ptr.Int64(defaultConcurrency)),
		numEndpoints:   1,
		maxConcurrency: defaultMaxConcurrency,
		want:           int(defaultConcurrency),
	}, {
		label:          "unlimited concurrency",
		revisionLister: revisionLister(testNamespace, testRevision, ptr.Int64(0)),
		numEndpoints:   1,
		maxConcurrency: 100,
		want:           1, // We're using infinity breaker, which is 0-1 only.
	}, {
		label:          "non-existing revision",
		revisionLister: revisionLister("bogus-namespace", testRevision, ptr.Int64(defaultConcurrency)),
		numEndpoints:   1,
		maxConcurrency: defaultMaxConcurrency,
		want:           0,
		wantError:      true,
	}, {
		label:          "exceeds maxConcurrency",
		revisionLister: revisionLister(testNamespace, testRevision, ptr.Int64(defaultConcurrency)),
		numEndpoints:   1,
		maxConcurrency: 5,
		want:           5,
	}, {
		label:          "no endpoints",
		revisionLister: revisionLister(testNamespace, testRevision, ptr.Int64(1)),
		numEndpoints:   0,
		maxConcurrency: 5,
		want:           0,
	}, {
		label:          "no endpoints, unlimited concurrency",
		revisionLister: revisionLister(testNamespace, testRevision, ptr.Int64(0)),
		numEndpoints:   0,
		maxConcurrency: 5,
		want:           0,
	}, {
		label:          "container concurrency is nil",
		revisionLister: revisionLister(testNamespace, testRevision, nil),
		numEndpoints:   0,
		maxConcurrency: 5,
		want:           0,
	}}
	for _, s := range samples {
		t.Run(s.label, func(t *testing.T) {
			throttler := getThrottler(
				s.maxConcurrency,
				s.revisionLister,
				endpointsInformer(testNamespace, testRevision, 1),
				nil, /*sksLister*/
				TestLogger(t),
				initCapacity)

			err := throttler.UpdateCapacity(revID, s.numEndpoints)
			if err == nil && s.wantError {
				t.Errorf("UpdateCapacity() did not return an error")
			} else if err != nil && !s.wantError {
				t.Errorf("UpdateCapacity() = `%v`, wanted no error", err)
			}
			if s.want > 0 {
				if got := throttler.breakers[revID].Capacity(); got != s.want {
					t.Errorf("breakers[revID].Capacity() = %d, want %d", got, s.want)
				}
			}
		})
	}
}

func TestThrottlerActivatorEndpoints(t *testing.T) {
	const (
		updatePollInterval = 10 * time.Millisecond
		updatePollTimeout  = 3 * time.Second
	)

	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testRevision,
			Namespace: testNamespace,
		},
		Subsets: endpointsSubset(1, 1),
	}
	fake := kubefake.NewSimpleClientset(ep)
	informer := kubeinformers.NewSharedInformerFactory(fake, 0)
	endpoints := informer.Core().V1().Endpoints()
	endpoints.Informer().GetIndexer().Add(ep)

	stopCh := make(chan struct{})
	defer close(stopCh)
	controller.StartInformers(stopCh, endpoints.Informer())

	scenarios := []struct {
		name                string
		activatorCount      int
		revisionConcurrency int64
		wantCapacity        int
	}{{
		name:                "less activators, more cc",
		activatorCount:      2,
		revisionConcurrency: defaultConcurrency,
		wantCapacity:        5, //revConcurrency / activatorCount
	}, {
		name:                "many activators, less cc",
		activatorCount:      3,
		revisionConcurrency: int64(2),
		wantCapacity:        1,
	}}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			activatorEp := &corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      K8sServiceName,
					Namespace: system.Namespace(),
				},
				Subsets: endpointsSubset(1, s.activatorCount),
			}

			throttler := getThrottler(
				defaultMaxConcurrency,
				revisionLister(testNamespace, testRevision, ptr.Int64(s.revisionConcurrency)),
				endpoints,
				sksLister(testNamespace, testRevision),
				TestLogger(t),
				initCapacity,
			)
			throttler.UpdateCapacity(revID, 1) // This sets the initial breaker

			fake.CoreV1().Endpoints(activatorEp.Namespace).Create(activatorEp)
			endpoints.Informer().GetIndexer().Add(activatorEp)

			breaker := throttler.breakers[RevisionID{Name: testRevision, Namespace: testNamespace}]

			if err := wait.PollImmediate(updatePollInterval, updatePollTimeout, func() (bool, error) {
				return breaker.Capacity() == s.wantCapacity, nil
			}); err != nil {
				t.Errorf("Capacity() = %d, want %d", breaker.Capacity(), s.wantCapacity)
			}
		})
	}
}

func TestThrottlerTry(t *testing.T) {
	defer ClearAll()
	samples := []struct {
		label             string
		addCapacity       bool
		revisionLister    servinglisters.RevisionLister
		endpointsInformer corev1informers.EndpointsInformer
		sksLister         netlisters.ServerlessServiceLister
		wantCalls         int
		wantError         bool
	}{{
		label:             "all good",
		addCapacity:       true,
		revisionLister:    revisionLister(testNamespace, testRevision, ptr.Int64(defaultConcurrency)),
		endpointsInformer: endpointsInformer(testNamespace, testRevision, 0),
		sksLister:         sksLister(testNamespace, testRevision),
		wantCalls:         1,
	}, {
		label:             "non-existing revision",
		addCapacity:       true,
		revisionLister:    revisionLister("bogus-namespace", testRevision, ptr.Int64(defaultConcurrency)),
		endpointsInformer: endpointsInformer(testNamespace, testRevision, 0),
		sksLister:         sksLister(testNamespace, testRevision),
		wantCalls:         0,
		wantError:         true,
	}, {
		label:             "error getting SKS",
		revisionLister:    revisionLister(testNamespace, testRevision, ptr.Int64(defaultConcurrency)),
		endpointsInformer: endpointsInformer(testNamespace, testRevision, 1),
		sksLister:         sksLister("bogus-namespace", testRevision),
		wantCalls:         0,
		wantError:         true,
	}, {
		label:             "error getting endpoint",
		revisionLister:    revisionLister(testNamespace, testRevision, ptr.Int64(defaultConcurrency)),
		endpointsInformer: endpointsInformer("bogus-namespace", testRevision, 0),
		sksLister:         sksLister(testNamespace, testRevision),
		wantCalls:         0,
		wantError:         true,
	}}
	for _, s := range samples {
		t.Run(s.label, func(t *testing.T) {
			var called int
			throttler := getThrottler(
				defaultMaxConcurrency,
				s.revisionLister,
				s.endpointsInformer,
				s.sksLister,
				TestLogger(t),
				initCapacity)
			if s.addCapacity {
				throttler.UpdateCapacity(revID, 1)
			}
			err := throttler.Try(context.Background(), revID, func() {
				called++
			})
			if err == nil && s.wantError {
				t.Errorf("UpdateCapacity() did not return an error")
			} else if err != nil && !s.wantError {
				t.Errorf("UpdateCapacity() = %v, wanted no error", err)
			}
			if got, want := called, s.wantCalls; got != want {
				t.Errorf("Unexpected number of function runs in Try = %d, want: %d", got, want)
			}
		})
	}
}

func TestThrottlerTryOverload(t *testing.T) {
	maxConcurrency := 1
	initialCapacity := 1
	queueLength := 1
	th := getThrottler(
		maxConcurrency,
		revisionLister(testNamespace, testRevision, ptr.Int64(1)),
		endpointsInformer(testNamespace, testRevision, 1),
		sksLister(testNamespace, testRevision),
		TestLogger(t),
		initialCapacity)

	doneCh := make(chan struct{})
	errCh := make(chan error)

	// Make one more request than allowed.
	allowedRequests := initialCapacity + queueLength
	for i := 0; i < allowedRequests+1; i++ {
		go func() {
			err := th.Try(context.Background(), revID, func() {
				doneCh <- struct{}{} // Blocks forever
			})
			if err != nil {
				errCh <- err
			}
		}()
	}

	if err := <-errCh; err != ErrActivatorOverload {
		t.Errorf("error = %v, want: %v", err, ErrActivatorOverload)
	}

	successfulRequests := 0
	for i := 0; i < allowedRequests; i++ {
		select {
		case <-doneCh:
			successfulRequests++
		case <-errCh:
			t.Errorf("Only one request should fail.")
		}
	}

	if successfulRequests != allowedRequests {
		t.Errorf("successfulRequests = %d, want: %d", successfulRequests, allowedRequests)
	}
}

func TestThrottlerRemove(t *testing.T) {
	throttler := getThrottler(
		defaultMaxConcurrency,
		revisionLister(testNamespace, testRevision, ptr.Int64(defaultConcurrency)),
		endpointsInformer(testNamespace, testRevision, 0),
		sksLister(testNamespace, testRevision),
		TestLogger(t),
		initCapacity)

	throttler.breakers[revID] = queue.NewBreaker(throttler.breakerParams)
	if got := len(throttler.breakers); got != 1 {
		t.Errorf("Number of Breakers created = %d, want: 1", got)
	}

	throttler.Remove(revID)
	if got := len(throttler.breakers); got != 0 {
		t.Errorf("Number of Breakers created = %d, want: %d", got, 0)
	}
}

func TestHelper_ReactToEndpoints(t *testing.T) {
	const updatePollInterval = 10 * time.Millisecond
	const updatePollTimeout = 3 * time.Second

	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testRevision + "-service",
			Namespace: testNamespace,
			Labels: map[string]string{
				serving.RevisionUID:       "test",
				networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
				serving.RevisionLabelKey:  testRevision,
			},
		},
		Subsets: endpointsSubset(0, 1),
	}

	fake := kubefake.NewSimpleClientset()
	informer := kubeinformers.NewSharedInformerFactory(fake, 0)
	endpointsInformer := informer.Core().V1().Endpoints()

	stopCh := make(chan struct{})
	defer close(stopCh)
	controller.StartInformers(stopCh, endpointsInformer.Informer())

	throttler := getThrottler(
		200,
		revisionLister(testNamespace, testRevision, ptr.Int64(defaultConcurrency)),
		endpointsInformer,
		sksLister(testNamespace, testRevision),
		TestLogger(t),
		initCapacity)

	// Breaker is created with 0 ready addresses.
	fake.CoreV1().Endpoints(ep.Namespace).Create(ep)
	wait.PollImmediate(updatePollInterval, updatePollTimeout, func() (bool, error) {
		return breakerCount(throttler) == 1, nil
	})
	if got := breakerCount(throttler); got != 1 {
		t.Errorf("breakerCount() = %d, want 1", got)
	}
	breaker := throttler.breakers[RevisionID{Name: testRevision, Namespace: testNamespace}]
	if got := breaker.Capacity(); got != 0 {
		t.Errorf("Capacity() = %d, want 0", got)
	}

	// Add 10 addresses, 10 * 10 = 100 = new capacity
	newEp := ep.DeepCopy()
	newEp.Subsets = endpointsSubset(10, 1)
	fake.Core().Endpoints(ep.Namespace).Update(newEp)
	wait.PollImmediate(updatePollInterval, updatePollTimeout, func() (bool, error) {
		return int64(breaker.Capacity()) == 10*defaultConcurrency, nil
	})
	if got, want := int64(breaker.Capacity()), 10*defaultConcurrency; got != want {
		t.Errorf("Capacity() = %d, want %d", got, want)
	}

	// Removing the endpoints causes the breaker to be removed.
	fake.Core().Endpoints(ep.Namespace).Delete(ep.Name, &metav1.DeleteOptions{})
	wait.PollImmediate(updatePollInterval, updatePollTimeout, func() (bool, error) {
		return breakerCount(throttler) == 0, nil
	})
	if got := breakerCount(throttler); got != 0 {
		t.Errorf("breakerCount() = %d, want 0", got)
	}
}

func revisionLister(namespace, name string, concurrency *int64) servinglisters.RevisionLister {
	rev := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.RevisionSpec{
			RevisionSpec: v1beta1.RevisionSpec{
				ContainerConcurrency: concurrency,
			},
		},
	}

	fake := servingfake.NewSimpleClientset(rev)
	informer := servinginformers.NewSharedInformerFactory(fake, 0)
	revisions := informer.Serving().V1alpha1().Revisions()
	revisions.Informer().GetIndexer().Add(rev)

	return revisions.Lister()
}

func endpointsInformer(namespace, name string, count int) corev1informers.EndpointsInformer {
	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subsets: endpointsSubset(count, 1),
	}

	fake := kubefake.NewSimpleClientset(ep)
	informer := kubeinformers.NewSharedInformerFactory(fake, 0)
	endpoints := informer.Core().V1().Endpoints()
	endpoints.Informer().GetIndexer().Add(ep)

	return endpoints
}

func sksLister(namespace, name string) netlisters.ServerlessServiceLister {
	sks := &nv1a1.ServerlessService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: nv1a1.ServerlessServiceStatus{
			PrivateServiceName: name,
			ServiceName:        helpers.AppendRandomString(name),
		},
	}

	fake := servingfake.NewSimpleClientset(sks)
	informer := servinginformers.NewSharedInformerFactory(fake, 0)
	skss := informer.Networking().V1alpha1().ServerlessServices()
	skss.Informer().GetIndexer().Add(sks)

	return skss.Lister()
}

func getThrottler(
	maxConcurrency int,
	revisionLister servinglisters.RevisionLister,
	endpointsInformer corev1informers.EndpointsInformer,
	sksLister netlisters.ServerlessServiceLister,
	logger *zap.SugaredLogger,
	initCapacity int) *Throttler {
	params := queue.BreakerParams{
		QueueDepth:      1,
		MaxConcurrency:  maxConcurrency,
		InitialCapacity: initCapacity,
	}
	return NewThrottler(params, endpointsInformer, sksLister, revisionLister, logger)
}

func breakerCount(t *Throttler) int {
	t.breakersMux.Lock()
	defer t.breakersMux.Unlock()
	return len(t.breakers)
}

func endpointsSubset(hostsPerSubset, subsets int) []corev1.EndpointSubset {
	resp := []corev1.EndpointSubset{}
	if hostsPerSubset > 0 {
		addresses := make([]corev1.EndpointAddress, hostsPerSubset)
		subset := corev1.EndpointSubset{Addresses: addresses}
		for s := 0; s < subsets; s++ {
			resp = append(resp, subset)
		}
		return resp
	}
	return resp
}

func TestInfiniteBreaker(t *testing.T) {
	b := &infiniteBreaker{
		broadcast: make(chan struct{}),
	}

	// Verify initial condition.
	if got, want := b.Capacity(), 0; got != want {
		t.Errorf("Cap=%d, want: %d", got, want)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if b.Maybe(ctx, nil) {
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
		if !b.Maybe(ctx, func() { res = true }) {
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
	if b.Maybe(ctx, nil) {
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
	if !b.Maybe(ctx, func() { res = true }) {
		t.Error("Should have succeeded, but didn't")
	}
	if !res {
		t.Error("thunk was not invoked")
	}
}

func revisionListerN(namespace, name string, count int) servinglisters.RevisionLister {
	revs := make([]runtime.Object, count)
	for i := 0; i < count; i++ {
		revs[i] = &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + strconv.Itoa(i),
				Namespace: namespace,
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: ptr.Int64(0),
				},
			},
		}
	}
	fake := servingfake.NewSimpleClientset(revs...)
	informer := servinginformers.NewSharedInformerFactory(fake, 0)
	revisions := informer.Serving().V1alpha1().Revisions()
	for i := 0; i < count; i++ {
		revisions.Informer().GetIndexer().Add(revs[i])
	}
	return revisions.Lister()
}

func BenchmarkThrottler(b *testing.B) {
	const numRevs = 10000
	throttler := getThrottler(
		defaultMaxConcurrency,
		revisionListerN(testNamespace, testRevision, numRevs),
		endpointsInformer(testNamespace, testRevision, 0),
		sksLister(testNamespace, testRevision),
		nil,
		initCapacity)

	rIDs := make([]RevisionID, numRevs)
	for i := 0; i < numRevs; i++ {
		rID := RevisionID{testNamespace, testRevision + strconv.Itoa(i)}
		rIDs[i] = rID
		throttler.UpdateCapacity(rID, 1)
	}

	rand.Seed(time.Now().Unix())
	for _, parallelism := range []int{1, 10, 100, 1000, 10000} {
		for _, numRevs := range []int{1, 10, 100, 1000, 10000} {
			b.Run(strconv.Itoa(parallelism)+"-"+strconv.Itoa(numRevs), func(b *testing.B) {
				b.SetParallelism(parallelism)
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						revID := rIDs[rand.Intn(numRevs)]
						if err := throttler.Try(context.Background(), revID, func() {}); err != nil {
							b.Errorf("Try() unexpectedly returned an error: %v", err)
						}
					}
				})
			})
		}
	}
}

func TestProbeCache(t *testing.T) {
	cache := &probeCache{
		probes: sets.NewString(),
	}
	revID := RevisionID{}
	if !cache.should(revID) {
		t.Error("should returned true")
	}
	cache.mark(revID)
	if cache.should(revID) {
		t.Error("should returned false")
	}
	cache.unmark(revID)
	if !cache.should(revID) {
		t.Error("should returned true")
	}
}

func TestThrottlerCache(t *testing.T) {
	thtl := getThrottler(
		int(defaultConcurrency),
		revisionLister(testNamespace, testRevision, ptr.Int64(defaultConcurrency)),
		endpointsInformer(testNamespace, testRevision, 1),
		nil, /*sksLister*/
		TestLogger(t),
		0)
	revID = RevisionID{}
	if !thtl.ShouldProbe(revID) {
		t.Error("Expected request to probe")
	}
	thtl.MarkProbe(revID)
	if thtl.ShouldProbe(revID) {
		t.Error("Expected no need to probe")
	}
	thtl.UpdateCapacity(revID, 0)
	if !thtl.ShouldProbe(revID) {
		t.Error("Expected request to probe after scale to 0")
	}
	thtl.UpdateCapacity(revID, 1)
	if !thtl.ShouldProbe(revID) {
		t.Error("Expected request to probe after scale to 1, but before any probe happened")
	}
}
