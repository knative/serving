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
	"errors"
	"testing"

	"go.uber.org/zap"

	. "github.com/knative/pkg/logging/testing"
	"github.com/knative/pkg/test/helpers"
	nv1a1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	servingfake "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions"
	netlisters "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	servinglisters "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/queue"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

var (
	testNamespace = "good-namespace"
	testRevision  = "good-name"
	revID         = RevisionID{testNamespace, testRevision}
	errTest       = errors.New("some error")
)

const (
	defaultMaxConcurrency = 1000
	initCapacity          = 0
)

func revisionLister(namespace, name string, concurrency v1beta1.RevisionContainerConcurrencyType) servinglisters.RevisionLister {
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

func endpointsLister(namespace, name string, count int) corev1listers.EndpointsLister {
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

	return endpoints.Lister()
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

func TestThrottler_UpdateCapacity(t *testing.T) {
	samples := []struct {
		label          string
		revisionLister servinglisters.RevisionLister
		numEndpoints   int
		maxConcurrency int
		want           int
		wantError      bool
	}{{
		label:          "all good",
		revisionLister: revisionLister(testNamespace, testRevision, 10),
		numEndpoints:   1,
		maxConcurrency: defaultMaxConcurrency,
		want:           10,
	}, {
		label:          "unlimited concurrency",
		revisionLister: revisionLister(testNamespace, testRevision, 0),
		numEndpoints:   1,
		maxConcurrency: 100,
		want:           100,
	}, {
		label:          "non-existing revision",
		revisionLister: revisionLister("bogus-namespace", testRevision, 10),
		numEndpoints:   1,
		maxConcurrency: defaultMaxConcurrency,
		want:           0,
		wantError:      true,
	}, {
		label:          "exceeds maxConcurrency",
		revisionLister: revisionLister(testNamespace, testRevision, 10),
		numEndpoints:   1,
		maxConcurrency: 5,
		want:           5,
	}, {
		label:          "no endpoints",
		revisionLister: revisionLister(testNamespace, testRevision, 1),
		numEndpoints:   0,
		maxConcurrency: 5,
		want:           0,
	}, {
		label:          "no endpoints, unlimited concurrency",
		revisionLister: revisionLister(testNamespace, testRevision, 0),
		numEndpoints:   0,
		maxConcurrency: 5,
		want:           0,
	}}
	for _, s := range samples {
		t.Run(s.label, func(t *testing.T) {
			throttler := getThrottler(
				s.maxConcurrency,
				s.revisionLister,
				nil, /*endpointsLister*/
				nil, /*sksLister*/
				TestLogger(t),
				initCapacity)

			err := throttler.UpdateCapacity(revID, s.numEndpoints)
			if err == nil && s.wantError {
				t.Errorf("UpdateCapacity() did not return an error")
			} else if err != nil && !s.wantError {
				t.Errorf("UpdateCapacity() = %v, wanted no error", err)
			}
			if s.want > 0 {
				if got := throttler.breakers[revID].Capacity(); got != s.want {
					t.Errorf("breakers[revID].Capacity() = %d, want %d", got, s.want)
				}
			}
		})
	}
}

func TestThrottler_Try(t *testing.T) {
	defer ClearAll()
	samples := []struct {
		label           string
		addCapacity     bool
		revisionLister  servinglisters.RevisionLister
		endpointsLister corev1listers.EndpointsLister
		sksLister       netlisters.ServerlessServiceLister
		wantCalls       int
		wantError       bool
	}{{
		label:           "all good",
		addCapacity:     true,
		revisionLister:  revisionLister(testNamespace, testRevision, 10),
		endpointsLister: endpointsLister(testNamespace, testRevision, 0),
		sksLister:       sksLister(testNamespace, testRevision),
		wantCalls:       1,
	}, {
		label:           "non-existing revision",
		addCapacity:     true,
		revisionLister:  revisionLister("bogus-namespace", testRevision, 10),
		endpointsLister: endpointsLister(testNamespace, testRevision, 0),
		sksLister:       sksLister(testNamespace, testRevision),
		wantCalls:       0,
		wantError:       true,
	}, {
		label:           "error getting SKS",
		revisionLister:  revisionLister(testNamespace, testRevision, 10),
		endpointsLister: endpointsLister(testNamespace, testRevision, 1),
		sksLister:       sksLister("bogus-namespace", testRevision),
		wantCalls:       0,
		wantError:       true,
	}, {
		label:           "error getting endpoint",
		revisionLister:  revisionLister(testNamespace, testRevision, 10),
		endpointsLister: endpointsLister("bogus-namespace", testRevision, 0),
		sksLister:       sksLister(testNamespace, testRevision),
		wantCalls:       0,
		wantError:       true,
	}}
	for _, s := range samples {
		t.Run(s.label, func(t *testing.T) {
			var called int
			throttler := getThrottler(
				defaultMaxConcurrency,
				s.revisionLister,
				s.endpointsLister,
				s.sksLister,
				TestLogger(t),
				initCapacity)

			if s.addCapacity {
				throttler.UpdateCapacity(revID, 1)
			}
			err := throttler.Try(revID, func() {
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

func TestThrottler_TryOverload(t *testing.T) {
	maxConcurrency := 1
	initialCapacity := 1
	queueLength := 1
	th := getThrottler(
		maxConcurrency,
		revisionLister(testNamespace, testRevision, 1),
		endpointsLister(testNamespace, testRevision, 1),
		sksLister(testNamespace, testRevision),
		TestLogger(t),
		initialCapacity)

	doneCh := make(chan struct{})
	errCh := make(chan error)

	// Make one more request than allowed.
	allowedRequests := initialCapacity + queueLength
	for i := 0; i < allowedRequests+1; i++ {
		go func() {
			err := th.Try(revID, func() {
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

func TestUpdateEndpoints(t *testing.T) {
	revisionConcurrency := 10

	samples := []struct {
		label          string
		endpointsAfter int
		wantCapacity   int
		initCapacity   int
	}{{
		label:          "add single endpoint",
		endpointsAfter: 1,
		wantCapacity:   1 * revisionConcurrency,
	}, {
		label:          "add several endpoints",
		endpointsAfter: 2,
		wantCapacity:   2 * revisionConcurrency,
	}, {
		label:          "do nothing",
		endpointsAfter: 1,
		wantCapacity:   1 * revisionConcurrency,
		initCapacity:   10,
	}, {
		label:          "reduce endpoints",
		endpointsAfter: 0,
		wantCapacity:   0,
		initCapacity:   10,
	}, {
		label:          "exceed max concurrency",
		endpointsAfter: 101 * revisionConcurrency,
		wantCapacity:   defaultMaxConcurrency,
	}}

	for _, s := range samples {
		t.Run(s.label, func(t *testing.T) {
			throttler := getThrottler(
				defaultMaxConcurrency,
				revisionLister(testNamespace, testRevision, v1beta1.RevisionContainerConcurrencyType(revisionConcurrency)),
				endpointsLister(testNamespace, testRevision, 0),
				sksLister(testNamespace, testRevision),
				TestLogger(t),
				s.initCapacity)

			breaker := queue.NewBreaker(throttler.breakerParams)
			throttler.breakers[revID] = breaker
			endpointsAfter := corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      helpers.AppendRandomString(revID.Name),
					Namespace: revID.Namespace,
				},
				Subsets: endpointsSubset(s.endpointsAfter, 1),
			}
			throttler.UpdateEndpoints(&endpointsAfter)
			if got := breaker.Capacity(); got != s.wantCapacity {
				t.Errorf("Breaker capacity = %d, want: %d", got, s.wantCapacity)
			}
		})
	}
}

func TestThrottler_Remove(t *testing.T) {
	throttler := getThrottler(
		defaultMaxConcurrency,
		revisionLister(testNamespace, testRevision, 10),
		endpointsLister(testNamespace, testRevision, 0),
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

func TestHelper_DeleteBreaker(t *testing.T) {
	throttler := getThrottler(
		20,
		revisionLister(testNamespace, testRevision, 10),
		endpointsLister(testNamespace, testRevision, 0),
		sksLister(testNamespace, testRevision),
		TestLogger(t),
		initCapacity)

	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      revID.Name + "-suffix",
			Namespace: revID.Namespace,
		},
	}
	revID := RevisionID{Namespace: revID.Namespace, Name: revID.Name}
	throttler.breakers[revID] = queue.NewBreaker(throttler.breakerParams)
	if got := len(throttler.breakers); got != 1 {
		t.Errorf("Breaker map size got %d, want: 1", got)
	}

	throttler.DeleteBreaker(endpoints)
	if len(throttler.breakers) != 0 {
		t.Errorf("Breaker map is not empty, got: %v", throttler.breakers)
	}
}

func getThrottler(
	maxConcurrency int,
	revisionLister servinglisters.RevisionLister,
	endpointsLister corev1listers.EndpointsLister,
	sksLister netlisters.ServerlessServiceLister,
	logger *zap.SugaredLogger,
	initCapacity int) *Throttler {
	params := queue.BreakerParams{
		QueueDepth:      1,
		MaxConcurrency:  maxConcurrency,
		InitialCapacity: initCapacity,
	}
	return NewThrottler(params, endpointsLister, sksLister, revisionLister, logger)
}

func endpointsSubset(hostsPerSubset, subsets int) []v1.EndpointSubset {
	resp := []v1.EndpointSubset{}
	if hostsPerSubset > 0 {
		addresses := make([]v1.EndpointAddress, hostsPerSubset)
		subset := v1.EndpointSubset{Addresses: addresses}
		for s := 0; s < subsets; s++ {
			resp = append(resp, subset)
		}
		return resp
	}
	return resp
}
