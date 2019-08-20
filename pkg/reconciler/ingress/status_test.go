/*
Copyright 2019 The Knative Authors.

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

package ingress

import (
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"knative.dev/pkg/apis/istio/v1alpha3"
	istiolisters "knative.dev/pkg/client/listers/istio/v1alpha3"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler/ingress/resources"
)

func TestIsReadyFailures(t *testing.T) {
	vs := &v1alpha3.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "whatever",
		},
	}

	tests := []struct {
		name          string
		vsSpec        v1alpha3.VirtualServiceSpec
		gatewayLister istiolisters.GatewayLister
		podLister     corev1listers.PodLister
	}{{
		name: "multiple probes",
		vsSpec: v1alpha3.VirtualServiceSpec{
			Hosts: []string{"foobar" + resources.ProbeHostSuffix, "barbaz" + resources.ProbeHostSuffix},
		},
	}, {
		name: "no probe",
		vsSpec: v1alpha3.VirtualServiceSpec{
			Hosts: []string{"foobar.com"},
		},
	}, {
		name: "invalid gateway",
		vsSpec: v1alpha3.VirtualServiceSpec{
			Gateways: []string{"not/valid/gateway"},
			Hosts:    []string{"foobar" + resources.ProbeHostSuffix},
		},
	}, {
		name: "gateway error",
		vsSpec: v1alpha3.VirtualServiceSpec{
			Gateways: []string{"knative/ingress-gateway"},
			Hosts:    []string{"foobar" + resources.ProbeHostSuffix},
		},
		gatewayLister: &fakeGatewayLister{fails: true},
	}, {
		name: "pod error",
		vsSpec: v1alpha3.VirtualServiceSpec{
			Gateways: []string{"default/gateway"},
			Hosts:    []string{"foobar" + resources.ProbeHostSuffix},
		},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1alpha3.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: v1alpha3.GatewaySpec{
					Servers: []v1alpha3.Server{{
						Hosts: []string{"*"},
						Port: v1alpha3.Port{
							Number:   80,
							Protocol: v1alpha3.ProtocolHTTP,
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		podLister: &fakePodLister{fails: true},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prober := NewStatusProber(
				zaptest.NewLogger(t).Sugar(),
				test.gatewayLister,
				test.podLister,
				network.NewAutoTransport,
				func(vs *v1alpha3.VirtualService) {})
			copy := vs.DeepCopy()
			copy.Spec = test.vsSpec
			_, err := prober.IsReady(copy)
			if err == nil {
				t.Errorf("expected an error, got nil")
			}
		})
	}
}

func TestProbeLifecycle(t *testing.T) {
	vs := &v1alpha3.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "whatever",
		},
		Spec: v1alpha3.VirtualServiceSpec{
			Gateways: []string{"gateway", "mesh"},
		},
	}

	host, err := resources.InsertProbe(vs)
	if err != nil {
		t.Fatalf("failed to insert probe route: %v", err)
	}

	// Simulate no matching route on the first call and matching in subsequent requests
	hosts := make(chan string, 1)
	hosts <- "foobar.com"
	go func() {
		for {
			hosts <- host
		}
	}()

	requests := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != <-hosts {
			w.WriteHeader(404)
		}
		requests <- struct{}{}
	}))
	defer ts.Close()
	url, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", ts.URL, err)
	}

	ready := make(chan *v1alpha3.VirtualService)
	prober := NewStatusProber(
		zaptest.NewLogger(t).Sugar(),
		&fakeGatewayLister{
			gateways: []*v1alpha3.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: v1alpha3.GatewaySpec{
					Servers: []v1alpha3.Server{{
						Hosts: []string{"*"},
						Port: v1alpha3.Port{
							Number:   80,
							Protocol: v1alpha3.ProtocolHTTP,
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		&fakePodLister{
			pods: []*v1.Pod{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Status: v1.PodStatus{
					PodIP: url.Host,
				},
			}},
		},
		network.NewAutoTransport,
		func(vs *v1alpha3.VirtualService) {
			ready <- vs
		})

	prober.stateExpiration = 2 * time.Second
	prober.cleanupPeriod = 500 * time.Millisecond

	done := make(chan struct{})
	defer close(done)
	prober.Start(done)

	// The first call to IsReady must succeed and return false
	ok, err := prober.IsReady(vs)
	if err != nil {
		t.Fatalf("IsReady failed: %v", err)
	}
	if ok {
		t.Fatalf("IsReady returned %v, want: %v", ok, false)
	}

	// Wait for the first request (failing) to be executed
	<-requests

	// Wait for the second request (success) to be executed
	<-requests

	// Wait for the probing to eventually succeed
	<-ready

	// The subsequent calls to IsReady must succeed and return true
	for i := 0; i < 5; i++ {
		if ok, err = prober.IsReady(vs); err != nil {
			t.Fatalf("IsReady failed: %v", err)
		} else if !ok {
			t.Fatalf("IsReady returned %v, want: %v", ok, false)
		}

		time.Sleep(prober.cleanupPeriod)
	}

	select {
	// Wait for the cleanup to happen
	case <-time.After(prober.stateExpiration + prober.cleanupPeriod):
		break
	// Validate that no requests were issued (cached)
	case <-requests:
		t.Fatal("an unexpected request was received")
	}

	// The state has expired and been removed
	ok, err = prober.IsReady(vs)
	if err != nil {
		t.Fatalf("IsReady failed: %v", err)
	}
	if ok {
		t.Fatalf("IsReady returned %v, want: %v", ok, false)
	}

	// Wait for the first request (success) to be executed
	<-requests

	// Wait for the probing to eventually succeed
	<-ready
}

type fakeGatewayLister struct {
	gateways []*v1alpha3.Gateway
	fails    bool
}

func (l *fakeGatewayLister) Gateways(namespace string) istiolisters.GatewayNamespaceLister {
	if l.fails {
		return &fakeGatewayNamespaceLister{fails: true}
	}

	var matches []*v1alpha3.Gateway
	for _, gateway := range l.gateways {
		if gateway.Namespace == namespace {
			matches = append(matches, gateway)
		}
	}
	return &fakeGatewayNamespaceLister{
		gateways: matches,
	}
}

func (l *fakeGatewayLister) List(selector labels.Selector) (ret []*v1alpha3.Gateway, err error) {
	log.Panic("not implemented")
	return nil, nil
}

type fakeGatewayNamespaceLister struct {
	gateways []*v1alpha3.Gateway
	fails    bool
}

func (l *fakeGatewayNamespaceLister) List(selector labels.Selector) (ret []*v1alpha3.Gateway, err error) {
	log.Panic("not implemented")
	return nil, nil
}

func (l *fakeGatewayNamespaceLister) Get(name string) (*v1alpha3.Gateway, error) {
	if l.fails {
		return nil, errors.New("failed to get Gateway")
	}

	for _, gateway := range l.gateways {
		if gateway.Name == name {
			return gateway, nil
		}
	}
	return nil, errors.New("not found")
}

type fakePodLister struct {
	pods  []*v1.Pod
	fails bool
}

func (l *fakePodLister) List(selector labels.Selector) (ret []*v1.Pod, err error) {
	if l.fails {
		return nil, errors.New("failed to get Pod")
	}
	// TODO(bancel): use selector
	return l.pods, nil
}

func (l *fakePodLister) Pods(namespace string) corev1listers.PodNamespaceLister {
	log.Panic("not implemented")
	return nil
}
