package activator

import (
	"testing"

	activatortest "github.com/knative/serving/pkg/activator/testing"
	netlisters "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/queue"
	corev1listers "k8s.io/client-go/listers/core/v1"
	. "knative.dev/pkg/logging/testing"
)

func TestBreakerUpdateSize(t *testing.T) {
	for _, test := range []struct {
		name           string
		size           int
		cc             int
		maxCC          int
		expectCapacity int
		probeResponses []activatortest.FakeResponse

		sksLister     netlisters.ServerlessServiceLister
		serviceLister corev1listers.ServiceLister
	}{{
		name:           "no capacity",
		size:           0,
		cc:             0,
		expectCapacity: 0,
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),
	}, {
		name:           "update size and cc",
		size:           2,
		cc:             3,
		maxCC:          10,
		expectCapacity: 6,
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),
	}, {
		name:           "maxCC limited",
		size:           10,
		cc:             5,
		maxCC:          20,
		expectCapacity: 20,
		sksLister:      sksLister(testNamespace, testRevision),
		serviceLister:  serviceLister(service(testNamespace, testRevision, "http")),
	}} {
		t.Run(test.name, func(t *testing.T) {
			// Setup fake roundtripper
			fakeRT := activatortest.FakeRoundTripper{}

			rt := network.RoundTripperFunc(fakeRT.RT)

			// Create a breaker which isnt running poll goroutine
			breaker := Breaker{
				Breaker:       queue.NewBreaker(queue.BreakerParams{QueueDepth: 10, MaxConcurrency: test.maxCC, InitialCapacity: 0}),
				maxCC:         test.maxCC,
				logger:        TestLogger(t),
				sksLister:     test.sksLister,
				serviceLister: test.serviceLister,
				transport:     rt,
			}
			breaker.sizeChangedCond.L = &breaker.mux

			breaker.UpdateSize(test.size, test.cc, 1)

			breaker.mux.Lock()
			breaker.probeAndSetConcurrency()
			breaker.mux.Unlock()

			if breaker.Breaker.Capacity() != test.expectCapacity {
				t.Errorf("Got capacity %d, expected %d", breaker.Breaker.Capacity(), test.expectCapacity)
			}
		})
	}
}
