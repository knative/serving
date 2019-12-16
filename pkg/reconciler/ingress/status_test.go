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
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/network/ingress"

	"go.uber.org/zap/zaptest"
	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1listers "k8s.io/client-go/listers/core/v1"
	istiolisters "knative.dev/serving/pkg/client/istio/listers/networking/v1alpha3"
	"knative.dev/serving/pkg/network"
)

var (
	iaTemplate = &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "whatever",
		},
		Spec: v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts: []string{
					"foo.bar.com",
				},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP:       &v1alpha1.HTTPIngressRuleValue{},
			}},
		},
	}
)

func TestIsReadyFailureAndSuccess(t *testing.T) {
	tests := []struct {
		name            string
		gateways        map[v1alpha1.IngressVisibility]sets.String
		gatewayLister   istiolisters.GatewayLister
		endpointsLister corev1listers.EndpointsLister
		serviceLister   corev1listers.ServiceLister
		succeeds        bool
	}{{
		name:     "invalid gateway",
		gateways: map[v1alpha1.IngressVisibility]sets.String{v1alpha1.IngressVisibilityExternalIP: sets.NewString("not/valid/gateway")},
	}, {
		name:     "unqualified gateway",
		gateways: map[v1alpha1.IngressVisibility]sets.String{v1alpha1.IngressVisibilityExternalIP: sets.NewString("gateway")},
	}, {
		name:          "gateway error",
		gateways:      map[v1alpha1.IngressVisibility]sets.String{v1alpha1.IngressVisibilityExternalIP: sets.NewString("default/gateway")},
		gatewayLister: &fakeGatewayLister{fails: true},
	}, {
		name:     "service error",
		gateways: map[v1alpha1.IngressVisibility]sets.String{v1alpha1.IngressVisibilityExternalIP: sets.NewString("default/gateway")},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1alpha3.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1alpha3.Gateway{
					Servers: []*istiov1alpha3.Server{{
						Hosts: []string{"*"},
						Port: &istiov1alpha3.Port{
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		serviceLister: &fakeServiceLister{fails: true},
	}, {
		name:     "endpoints error",
		gateways: map[v1alpha1.IngressVisibility]sets.String{v1alpha1.IngressVisibilityExternalIP: sets.NewString("default/gateway")},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1alpha3.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1alpha3.Gateway{
					Servers: []*istiov1alpha3.Server{{
						Hosts: []string{"*"},
						Port: &istiov1alpha3.Port{
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{fails: true},
	}, {
		name:     "service port not found",
		succeeds: true,
		gateways: map[v1alpha1.IngressVisibility]sets.String{v1alpha1.IngressVisibilityExternalIP: sets.NewString("default/gateway")},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1alpha3.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1alpha3.Gateway{
					Servers: []*istiov1alpha3.Server{{
						Hosts: []string{"*"},
						Port: &istiov1alpha3.Port{
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8080,
					}},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpoints: &v1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
			},
		},
	}, {
		name:     "no endpoints",
		succeeds: true,
		gateways: map[v1alpha1.IngressVisibility]sets.String{v1alpha1.IngressVisibilityExternalIP: sets.NewString("default/gateway")},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1alpha3.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1alpha3.Gateway{
					Servers: []*istiov1alpha3.Server{{
						Hosts: []string{"*"},
						Port: &istiov1alpha3.Port{
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "http",
						Port: 80,
					}},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{
			endpoints: &v1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{},
			},
		},
	}, {
		name:     "no services",
		succeeds: true,
		gateways: map[v1alpha1.IngressVisibility]sets.String{v1alpha1.IngressVisibilityExternalIP: sets.NewString("default/gateway")},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1alpha3.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1alpha3.Gateway{
					Servers: []*istiov1alpha3.Server{{
						Hosts: []string{"*"},
						Port: &istiov1alpha3.Port{
							Number:   80,
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		serviceLister:   &fakeServiceLister{},
		endpointsLister: &fakeEndpointsLister{},
	}, {
		name:     "unsupported protocol",
		succeeds: true,
		gateways: map[v1alpha1.IngressVisibility]sets.String{v1alpha1.IngressVisibilityExternalIP: sets.NewString("default/gateway")},
		gatewayLister: &fakeGatewayLister{
			gateways: []*v1alpha3.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1alpha3.Gateway{
					Servers: []*istiov1alpha3.Server{{
						Hosts: []string{"*"},
						Port: &istiov1alpha3.Port{
							Number:   80,
							Protocol: "Mongo",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		serviceLister: &fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "http",
						Port: 80,
					}},
				},
			}},
		},
		endpointsLister: &fakeEndpointsLister{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ia := iaTemplate.DeepCopy()
			prober := NewStatusProber(
				zaptest.NewLogger(t).Sugar(),
				test.gatewayLister,
				test.endpointsLister,
				test.serviceLister,
				func(ia *v1alpha1.Ingress) {})
			ok, err := prober.IsReady(ia, test.gateways)
			if !test.succeeds && err == nil {
				t.Errorf("expected an error, got nil")
			} else if test.succeeds && !ok {
				t.Errorf("expected %t, got %t", test.succeeds, ok)
			}
		})
	}
}

func TestProbeLifecycle(t *testing.T) {
	gw := map[v1alpha1.IngressVisibility]sets.String{
		v1alpha1.IngressVisibilityExternalIP: sets.NewString("default/gateway"),
	}

	ia := iaTemplate.DeepCopy()
	hash, err := ingress.InsertProbe(ia)
	if err != nil {
		t.Fatalf("failed to insert probe: %v", err)
	}

	// Simulate that the latest configuration is not applied yet by returning a different
	// hash once and then the by returning the expected hash.
	hashes := make(chan string, 1)
	hashes <- "not-the-hash-you-are-looking-for"
	go func() {
		for {
			hashes <- hash
		}
	}()

	// Dummy handler returning HTTP 500 (it should never be called during probing)
	dummyRequests := make(chan *http.Request)
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dummyRequests <- r
		w.WriteHeader(500)
	})

	// Actual probe handler used in Activator and Queue-Proxy
	probeHandler := network.NewProbeHandler(dummyHandler)

	// Dummy handler keeping track of received requests, mimicking AppendHeader of K-Network-Hash
	// and simulate a non-existing host by returning 404.
	probeRequests := make(chan *http.Request)
	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Host, "foo.bar.com") {
			w.WriteHeader(404)
			return
		}

		probeRequests <- r
		r.Header.Set(network.HashHeaderName, <-hashes)
		probeHandler.ServeHTTP(w, r)
	})

	ts := httptest.NewServer(finalHandler)
	defer ts.Close()
	url, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", ts.URL, err)
	}

	hostname := url.Hostname()
	port, err := strconv.Atoi(url.Port())
	if err != nil {
		t.Fatalf("failed to parse port %q: %v", url.Port(), err)
	}
	ready := make(chan *v1alpha1.Ingress)
	prober := NewStatusProber(
		zaptest.NewLogger(t).Sugar(),
		&fakeGatewayLister{
			gateways: []*v1alpha3.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1alpha3.Gateway{
					Servers: []*istiov1alpha3.Server{{
						Hosts: []string{"*"},
						Port: &istiov1alpha3.Port{
							Name:     "http",
							Number:   80,
							Protocol: "HTTP",
						},
					}, {
						Hosts: []string{"*"},
						Port: &istiov1alpha3.Port{
							Name:     "https",
							Number:   443,
							Protocol: "HTTPS",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		&fakeEndpointsLister{
			endpoints: &v1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Ports: []v1.EndpointPort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: int32(port),
					}},
					Addresses: []v1.EndpointAddress{{
						IP: hostname,
					}},
				}},
			},
		},
		&fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "real",
						Port: 80,
					}},
				},
			}},
		},
		func(ia *v1alpha1.Ingress) {
			ready <- ia
		})

	prober.stateExpiration = 2 * time.Second
	prober.cleanupPeriod = 500 * time.Millisecond

	done := make(chan struct{})
	defer close(done)
	prober.Start(done)

	// The first call to IsReady must succeed and return false
	ok, err := prober.IsReady(ia, gw)
	if err != nil {
		t.Fatalf("IsReady failed: %v", err)
	}
	if ok {
		t.Fatalf("IsReady returned %v, want: %v", ok, false)
	}

	const expHostHeader = "foo.bar.com:80"

	// Wait for the first (failing) and second (success) requests to be executed and validate Host header
	for i := 0; i < 2; i++ {
		req := <-probeRequests
		if req.Host != expHostHeader {
			t.Fatalf("Host header = %q, want %q", req.Host, expHostHeader)
		}
	}

	// Wait for the probing to eventually succeed
	<-ready

	// The subsequent calls to IsReady must succeed and return true
	for i := 0; i < 5; i++ {
		if ok, err = prober.IsReady(ia, gw); err != nil {
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
	// Validate that no probe requests were issued (cached)
	case <-probeRequests:
		t.Fatal("an unexpected probe request was received")
	// Validate that no requests went through the probe handler
	case <-dummyRequests:
		t.Fatal("an unexpected request went through the probe handler")
	}

	// The state has expired and been removed
	ok, err = prober.IsReady(ia, gw)
	if err != nil {
		t.Fatalf("IsReady failed: %v", err)
	}
	if ok {
		t.Fatalf("IsReady returned %v, want: %v", ok, false)
	}

	// Wait for the first request (success) to be executed
	<-probeRequests

	// Wait for the probing to eventually succeed
	<-ready

	select {
	// Validate that no requests went through the probe handler
	case <-dummyRequests:
		t.Fatal("an unexpected request went through the probe handler")
	default:
		break
	}
}

func TestCancellation(t *testing.T) {
	ia := iaTemplate.DeepCopy()
	gw := map[v1alpha1.IngressVisibility]sets.String{
		v1alpha1.IngressVisibilityExternalIP: sets.NewString("default/gateway"),
	}

	// Handler keeping track of received requests and mimicking an Ingress not ready
	requests := make(chan *http.Request, 100)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r
		w.WriteHeader(404)
	})

	ts := httptest.NewServer(handler)
	defer ts.Close()
	url, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", ts.URL, err)
	}

	port, err := strconv.Atoi(url.Port())
	if err != nil {
		t.Fatalf("failed to parse port %q: %v", url.Port(), err)
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "gateway",
		},
		Status: v1.PodStatus{
			PodIP: strings.Split(url.Host, ":")[0],
		},
	}

	ready := make(chan *v1alpha1.Ingress)
	prober := NewStatusProber(
		zaptest.NewLogger(t).Sugar(),
		&fakeGatewayLister{
			gateways: []*v1alpha3.Gateway{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: istiov1alpha3.Gateway{
					Servers: []*istiov1alpha3.Server{{
						Hosts: []string{"*"},
						Port: &istiov1alpha3.Port{
							Number:   uint32(port),
							Protocol: "HTTP",
						},
					}},
					Selector: map[string]string{
						"gwt": "istio",
					},
				},
			}},
		},
		&fakeEndpointsLister{
			endpoints: &v1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Subsets: []v1.EndpointSubset{{
					Addresses: []v1.EndpointAddress{{
						IP: url.Hostname(),
					}},
					Ports: []v1.EndpointPort{{
						Name: "http2",
						Port: int32(port),
					}},
				}},
			},
		},
		&fakeServiceLister{
			services: []*v1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "gateway",
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{
						Name: "bogus",
						Port: 8080,
					}, {
						Name: "http2",
						Port: int32(port),
					}},
				},
			}},
		},
		func(ia *v1alpha1.Ingress) {
			ready <- ia
		})

	done := make(chan struct{})
	defer close(done)
	prober.Start(done)

	ok, err := prober.IsReady(ia, gw)
	if err != nil {
		t.Fatalf("IsReady failed: %v", err)
	}
	if ok {
		t.Fatalf("IsReady returned %v, want: %v", ok, false)
	}

	// Wait for the first probe request
	<-requests

	// Create a new version of the Ingress
	ia = ia.DeepCopy()
	ia.Spec.Rules[0].Hosts[0] = "blabla.net"

	// Check that probing is unsuccessful
	select {
	case <-ready:
		t.Fatal("Probing succeeded while it should not have succeeded")
	default:
	}

	ok, err = prober.IsReady(ia, gw)
	if err != nil {
		t.Fatalf("IsReady failed: %v", err)
	}
	if ok {
		t.Fatalf("IsReady returned %v, want: %v", ok, false)
	}

	// Drain requests for the old version
	for req := range requests {
		log.Printf("req.Host: %s", req.Host)
		if strings.HasPrefix(req.Host, "blabla.net") {
			break
		}
	}

	// Cancel Pod probing
	prober.CancelPodProbing(pod)

	// Check that the requests were for the new version
	close(requests)
	for req := range requests {
		if !strings.HasPrefix(req.Host, "blabla.net") {
			t.Fatalf("Unexpected Host: want: %s, got: %s", "blabla.net", req.Host)
		}
	}
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

func (l *fakeGatewayLister) List(selector labels.Selector) ([]*v1alpha3.Gateway, error) {
	log.Panic("not implemented")
	return nil, nil
}

type fakeGatewayNamespaceLister struct {
	gateways []*v1alpha3.Gateway
	fails    bool
}

func (l *fakeGatewayNamespaceLister) List(selector labels.Selector) ([]*v1alpha3.Gateway, error) {
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

type fakeEndpointsLister struct {
	endpoints *v1.Endpoints
	fails     bool
}

func (l *fakeEndpointsLister) List(selector labels.Selector) ([]*v1.Endpoints, error) {
	log.Panic("not implemented")
	return nil, nil
}

func (l *fakeEndpointsLister) Endpoints(namespace string) corev1listers.EndpointsNamespaceLister {
	return l
}

func (l *fakeEndpointsLister) Get(name string) (*v1.Endpoints, error) {
	if l.fails {
		return nil, errors.New("failed to get Endpoints")
	}
	return l.endpoints, nil
}

type fakeServiceLister struct {
	services []*v1.Service
	fails    bool
}

func (l *fakeServiceLister) List(selector labels.Selector) ([]*v1.Service, error) {
	if l.fails {
		return nil, errors.New("failed to get Services")
	}
	// TODO(bancel): use selector
	return l.services, nil
}

func (l *fakeServiceLister) Services(namespace string) corev1listers.ServiceNamespaceLister {
	log.Panic("not implemented")
	return nil
}

func (l *fakeServiceLister) GetPodServices(pod *v1.Pod) ([]*v1.Service, error) {
	log.Panic("not implemented")
	return nil, nil
}
