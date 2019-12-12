/*
Copyright 2018 The Knative Authors

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
	"testing"

	"go.uber.org/zap/zaptest"
	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	v1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1listers "k8s.io/client-go/listers/core/v1"
	istiolisters "knative.dev/pkg/client/istio/listers/networking/v1alpha3"
)

func TestKeyLookup(t *testing.T) {
	tests := []struct {
		name            string
		gateway         string
		gatewayLister   istiolisters.GatewayLister
		endpointsLister corev1listers.EndpointsLister
		serviceLister   corev1listers.ServiceLister
		succeeds        bool
	}{{
		name:    "invalid gateway",
		gateway: "not/valid/gateway",
	}, {
		name:    "unqualified gateway",
		gateway: "gateway",
	}, {
		name:          "gateway error",
		gateway:       "default/gateway",
		gatewayLister: &fakeGatewayLister{fails: true},
	}, {
		name:    "service error",
		gateway: "default/gateway",
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
		name:    "endpoints error",
		gateway: "default/gateway",
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
		gateway:  "default/gateway",
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
		gateway:  "default/gateway",
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
		gateway:  "default/gateway",
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
		succeeds: false,
		gateway:  "default/gateway",
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
			kl := keyLookup(
				zaptest.NewLogger(t).Sugar(),
				test.gatewayLister,
				test.serviceLister,
				test.endpointsLister)
			_, _, _, err := kl(test.gateway)
			if !test.succeeds && err == nil {
				t.Errorf("expected an error, got nil")
			} else if test.succeeds && err != nil {
				t.Errorf("expected %t, got %v", test.succeeds, err)
			}
		})
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
