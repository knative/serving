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
	"net/http"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	"go.uber.org/zap"

	activatortest "github.com/knative/serving/pkg/activator/testing"
	"github.com/knative/serving/pkg/apis/networking"
	nv1a1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	servingfake "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions"
	netlisters "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	servinglisters "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/queue"
	"knative.dev/pkg/controller"
	. "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/system"
	"knative.dev/pkg/test/helpers"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	corev1listers "k8s.io/client-go/listers/core/v1"
	_ "knative.dev/pkg/system/testing"
)

const (
	testNamespace         = "good-namespace"
	testRevision          = "good-name"
	defaultMaxConcurrency = 1000
	initCapacity          = 0
)

var (
	revID = RevisionID{testNamespace, testRevision}
)

func TestThrottlerProbe(t *testing.T) {
	for _, test := range []struct {
		name string

		reqRevId       RevisionID
		probeResponses []activatortest.FakeResponse
		tryTimeout     time.Duration
		revisionLister servinglisters.RevisionLister
		sksLister      netlisters.ServerlessServiceLister
		serviceLister  corev1listers.ServiceLister

		expectProbeCnt   int
		expectTrySuccess bool
	}{{
		name: "successful probe",

		reqRevId:       RevisionID{testNamespace, testRevision},
		revisionLister: revisionLister(testNamespace, testRevision, 10),
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),

		expectProbeCnt:   1,
		tryTimeout:       10 * time.Millisecond,
		expectTrySuccess: true,
	}, {
		name: "slowly activate",

		reqRevId: RevisionID{testNamespace, testRevision},
		probeResponses: []activatortest.FakeResponse{{
			Code: http.StatusOK,
			Body: "activator",
		}, {
			Code: http.StatusOK,
			Body: queue.Name,
		}},
		revisionLister: revisionLister(testNamespace, testRevision, 10),
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),

		expectProbeCnt:   2,
		expectTrySuccess: true,
	}, {
		name: "never activate",

		reqRevId: RevisionID{testNamespace, testRevision},
		probeResponses: []activatortest.FakeResponse{{
			Code: http.StatusOK,
			Body: "activator",
		}},
		revisionLister: revisionLister(testNamespace, testRevision, 10),
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),

		tryTimeout:       10 * time.Millisecond,
		expectTrySuccess: false,
	}, {
		name: "probe servererror",

		reqRevId: RevisionID{testNamespace, testRevision},
		probeResponses: []activatortest.FakeResponse{{
			Code: http.StatusInternalServerError,
			Body: "failed",
		}},
		revisionLister: revisionLister(testNamespace, testRevision, 10),
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),

		tryTimeout:       10 * time.Millisecond,
		expectTrySuccess: false,
	}, {
		name: "missing revision namespace",

		reqRevId:       RevisionID{"missing-namespace", testRevision},
		revisionLister: revisionLister(testNamespace, testRevision, 10),
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),

		tryTimeout:       10 * time.Millisecond,
		expectTrySuccess: false,
	}, {
		name: "missing revision name",

		reqRevId:       RevisionID{testNamespace, "missing-name"},
		revisionLister: revisionLister(testNamespace, testRevision, 10),
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),

		tryTimeout:       10 * time.Millisecond,
		expectTrySuccess: false,
	}} {
		t.Run(test.name, func(t *testing.T) {
			// Setup transports.
			fakeRT := activatortest.FakeRoundTripper{
				ExpectHost:     "test-host",
				LockerCh:       nil,
				ProbeResponses: test.probeResponses,
				RequestResponse: &activatortest.FakeResponse{
					Err:  nil,
					Code: http.StatusOK,
					Body: "hello, world!",
				},
			}
			rt := network.RoundTripperFunc(fakeRT.RT)

			params := queue.BreakerParams{QueueDepth: 1000, MaxConcurrency: 1000, InitialCapacity: 0}
			throttler := NewThrottler(
				params,
				rt,
				endpointsInformer(testNamespace, testRevision, 1),
				test.sksLister,
				test.serviceLister,
				test.revisionLister,
				TestLogger(t))
			defer throttler.Shutdown()

			trySuccess := false
			throttler.Try(test.tryTimeout, test.reqRevId, func() {
				trySuccess = true
			})

			if trySuccess != test.expectTrySuccess {
				t.Errorf("Got try success %v, expected %v", trySuccess, test.expectTrySuccess)
			}
		})
	}
}

func TestThrottlerUpdateCapacity(t *testing.T) {
	samples := []struct {
		label          string
		revisionLister servinglisters.RevisionLister
		sksLister      netlisters.ServerlessServiceLister
		serviceLister  corev1listers.ServiceLister
		numEndpoints   int
		maxConcurrency int
		want           int
		wantError      bool
	}{{
		label:          "all good",
		revisionLister: revisionLister(testNamespace, testRevision, 10),
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),
		numEndpoints:   1,
		maxConcurrency: defaultMaxConcurrency,
		want:           10,
	}, {
		label:          "unlimited concurrency",
		revisionLister: revisionLister(testNamespace, testRevision, 0),
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),
		numEndpoints:   1,
		maxConcurrency: 100,
		want:           100,
	}, {
		label:          "non-existing revision",
		revisionLister: revisionLister("bogus-namespace", testRevision, 10),
		sksLister:      sksLister("bogus-namespace", testRevision),
		serviceLister:  serviceLister(service("bogus-namespace", testRevision, "http")),
		numEndpoints:   1,
		maxConcurrency: defaultMaxConcurrency,
		want:           0,
		wantError:      true,
	}, {
		label:          "exceeds maxConcurrency",
		revisionLister: revisionLister(testNamespace, testRevision, 10),
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),
		numEndpoints:   1,
		maxConcurrency: 5,
		want:           5,
	}, {
		label:          "no endpoints",
		revisionLister: revisionLister(testNamespace, testRevision, 1),
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),
		numEndpoints:   0,
		maxConcurrency: 5,
		want:           0,
	}, {
		label:          "no endpoints, unlimited concurrency",
		revisionLister: revisionLister(testNamespace, testRevision, 0),
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),
		numEndpoints:   0,
		maxConcurrency: 5,
		want:           0,
	}}
	for _, s := range samples {
		t.Run(s.label, func(t *testing.T) {
			// Setup our roundtripper
			fakeRT := activatortest.FakeRoundTripper{}
			rt := network.RoundTripperFunc(fakeRT.RT)

			throttler := getThrottler(
				rt,
				s.maxConcurrency,
				s.revisionLister,
				endpointsInformer(testNamespace, testRevision, 1),
				s.sksLister,
				s.serviceLister,
				TestLogger(t),
				initCapacity)
			defer throttler.Shutdown()

			err := throttler.UpdateCapacity(revID, s.numEndpoints)
			if err == nil && s.wantError {
				t.Errorf("UpdateCapacity() did not return an error")
			} else if err != nil && !s.wantError {
				t.Errorf("UpdateCapacity() = %v, wanted no error", err)
			}
			if s.want > 0 {
				func() {
					// Force probe / concurrency update in breaker
					breaker := throttler.breakers[revID]
					breaker.mux.Lock()
					defer breaker.mux.Unlock()
					breaker.probeAndSetConcurrency()

					if got := throttler.breakers[revID].Capacity(); got != s.want {
						t.Errorf("breakers[revID].Capacity() = %d, want %d", got, s.want)
					}
				}()
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
		revisionConcurrency int
		wantCapacity        int
	}{{
		name:                "less activators, more cc",
		activatorCount:      2,
		revisionConcurrency: 10,
		wantCapacity:        5, //revConcurrency / activatorCount
	}, {
		name:                "many activators, less cc",
		activatorCount:      3,
		revisionConcurrency: 2,
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

			fakeRT := activatortest.FakeRoundTripper{}
			rt := network.RoundTripperFunc(fakeRT.RT)
			throttler := getThrottler(
				rt,
				defaultMaxConcurrency,
				revisionLister(testNamespace, testRevision, v1beta1.RevisionContainerConcurrencyType(s.revisionConcurrency)),
				endpoints,
				sksLister(testNamespace, testRevision),
				serviceLister(service(testNamespace, testRevision, "http")),
				TestLogger(t),
				initCapacity,
			)
			defer throttler.Shutdown()
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
		serviceLister     corev1listers.ServiceLister
		wantCalls         int
		wantError         bool
	}{{
		label:             "all good",
		addCapacity:       true,
		revisionLister:    revisionLister(testNamespace, testRevision, 10),
		endpointsInformer: endpointsInformer(testNamespace, testRevision, 0),
		sksLister:         sksLister(testNamespace, testRevision),
		serviceLister:     serviceLister(service(testNamespace, testRevision, "http")),
		wantCalls:         1,
	}, {
		label:             "non-existing revision",
		addCapacity:       true,
		revisionLister:    revisionLister("bogus-namespace", testRevision, 10),
		endpointsInformer: endpointsInformer(testNamespace, testRevision, 0),
		sksLister:         sksLister(testNamespace, testRevision),
		serviceLister:     serviceLister(service(testNamespace, testRevision, "http")),
		wantCalls:         0,
		wantError:         true,
	}, {
		label:             "error getting SKS",
		revisionLister:    revisionLister(testNamespace, testRevision, 10),
		endpointsInformer: endpointsInformer(testNamespace, testRevision, 1),
		sksLister:         sksLister("bogus-namespace", testRevision),
		serviceLister:     serviceLister(service("bogus-namespace", testRevision, "http")),
		wantCalls:         0,
		wantError:         true,
	}, {
		label:             "error getting endpoint",
		revisionLister:    revisionLister(testNamespace, testRevision, 10),
		endpointsInformer: endpointsInformer("bogus-namespace", testRevision, 0),
		sksLister:         sksLister(testNamespace, testRevision),
		serviceLister:     serviceLister(service(testNamespace, testRevision, "http")),
		wantCalls:         0,
		wantError:         true,
	}}
	for _, s := range samples {
		t.Run(s.label, func(t *testing.T) {
			// Setup our roundtripper
			fakeRT := activatortest.FakeRoundTripper{}
			rt := network.RoundTripperFunc(fakeRT.RT)

			var called int
			throttler := getThrottler(
				rt,
				defaultMaxConcurrency,
				s.revisionLister,
				s.endpointsInformer,
				s.sksLister,
				s.serviceLister,
				TestLogger(t),
				initCapacity)

			if s.addCapacity {
				throttler.UpdateCapacity(revID, 1)
			}
			err := throttler.Try(0, revID, func() {
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

	fakeRT := activatortest.FakeRoundTripper{}
	rt := network.RoundTripperFunc(fakeRT.RT)

	th := getThrottler(
		rt,
		maxConcurrency,
		revisionLister(testNamespace, testRevision, 1),
		endpointsInformer(testNamespace, testRevision, 1),
		sksLister(testNamespace, testRevision),
		serviceLister(service(testNamespace, testRevision, "http")),
		TestLogger(t),
		initialCapacity)

	doneCh := make(chan struct{})
	errCh := make(chan error)

	// Make one more request than allowed.
	allowedRequests := initialCapacity + queueLength
	for i := 0; i < allowedRequests+1; i++ {
		go func() {
			err := th.Try(0, revID, func() {
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
		network.NewAutoTransport(),
		defaultMaxConcurrency,
		revisionLister(testNamespace, testRevision, 10),
		endpointsInformer(testNamespace, testRevision, 0),
		sksLister(testNamespace, testRevision),
		serviceLister(service(testNamespace, testRevision, "http")),
		TestLogger(t),
		initCapacity)

	throttler.breakers[revID] = NewBreaker(TestLogger(t), network.NewAutoTransport(), throttler.breakerParams, 1, sksLister(testNamespace, testRevision), serviceLister(service(testNamespace, testRevision, "http")), revID)
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

	// Setup our roundtripper
	fakeRT := activatortest.FakeRoundTripper{}
	rt := network.RoundTripperFunc(fakeRT.RT)

	throttler := getThrottler(
		rt,
		200,
		revisionLister(testNamespace, testRevision, 10),
		endpointsInformer,
		sksLister(testNamespace, testRevision),
		serviceLister(service(testNamespace, testRevision, "http")),
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
		return breaker.Capacity() == 100, nil
	})
	if got := breaker.Capacity(); got != 100 {
		t.Errorf("Capacity() = %d, want 100", got)
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

func service(namespace, name string, portName string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name: portName,
				Port: 8080,
			}},
		}}
}

func serviceLister(svcs ...*corev1.Service) corev1listers.ServiceLister {
	fake := kubefake.NewSimpleClientset()
	informer := kubeinformers.NewSharedInformerFactory(fake, 0)
	services := informer.Core().V1().Services()

	for _, svc := range svcs {
		fake.Core().Services(svc.Namespace).Create(svc)
		services.Informer().GetIndexer().Add(svc)
	}

	return services.Lister()
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
	transport http.RoundTripper,
	maxConcurrency int,
	revisionLister servinglisters.RevisionLister,
	endpointsInformer corev1informers.EndpointsInformer,
	sksLister netlisters.ServerlessServiceLister,
	serviceLister corev1listers.ServiceLister,
	logger *zap.SugaredLogger,
	initCapacity int) *Throttler {
	params := queue.BreakerParams{
		QueueDepth:      1,
		MaxConcurrency:  maxConcurrency,
		InitialCapacity: initCapacity,
	}
	return NewThrottler(params, transport, endpointsInformer, sksLister, serviceLister, revisionLister, logger)
}

func breakerCount(t *Throttler) int {
	t.breakersMux.Lock()
	defer t.breakersMux.Unlock()
	return len(t.breakers)
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
