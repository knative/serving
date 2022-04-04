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

package resources

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	pkgnet "knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/reconciler/serverlessservice/resources/names"
)

func sks(mod func(*v1alpha1.ServerlessService)) *v1alpha1.ServerlessService {
	base := &v1alpha1.ServerlessService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "melon",
			Name:      "collie",
			UID:       "1982",
			// Those labels are propagated from the Revision->PA.
			Labels: map[string]string{
				serving.RevisionLabelKey: "collie",
				serving.RevisionUID:      "1982",
			},
			Annotations: map[string]string{},
		},
		Spec: v1alpha1.ServerlessServiceSpec{
			ProtocolType: pkgnet.ProtocolHTTP1,
			Mode:         v1alpha1.SKSOperationModeServe,
		},
	}
	if mod != nil {
		mod(base)
	}
	return base
}

func eps(mod func(*corev1.Endpoints)) *corev1.Endpoints {
	base := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "melon",
			Name:      "collie",
			Labels: map[string]string{
				serving.RevisionLabelKey:  "collie",
				serving.RevisionUID:       "1982",
				networking.SKSLabelKey:    "collie",
				networking.ServiceTypeKey: "Public",
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         v1alpha1.SchemeGroupVersion.String(),
				Kind:               "ServerlessService",
				Name:               "collie",
				UID:                "1982",
				Controller:         ptr.Bool(true),
				BlockOwnerDeletion: ptr.Bool(true),
			}},
		},
	}
	if mod != nil {
		mod(base)
	}
	return base
}

func svc(t networking.ServiceType, mods ...func(*corev1.Service)) *corev1.Service {
	base := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "melon",
			Name:      "collie",
			Labels: map[string]string{
				// Those should be propagated.
				serving.RevisionLabelKey:  "collie",
				serving.RevisionUID:       "1982",
				networking.SKSLabelKey:    "collie",
				networking.ServiceTypeKey: string(t),
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         v1alpha1.SchemeGroupVersion.String(),
				Kind:               "ServerlessService",
				Name:               "collie",
				UID:                "1982",
				Controller:         ptr.Bool(true),
				BlockOwnerDeletion: ptr.Bool(true),
			}},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       pkgnet.ServicePortNameHTTP1,
				Protocol:   corev1.ProtocolTCP,
				Port:       pkgnet.ServiceHTTPPort,
				TargetPort: intstr.FromInt(networking.BackendHTTPPort),
			}, {
				Name:       pkgnet.ServicePortNameHTTPS,
				Protocol:   corev1.ProtocolTCP,
				Port:       pkgnet.ServiceHTTPSPort,
				TargetPort: intstr.FromInt(networking.BackendHTTPSPort),
			}},
		},
	}
	for _, mod := range mods {
		mod(base)
	}
	return base
}

func privateSvcMod(s *corev1.Service) {
	s.Name = names.PrivateService(s.Name)
	if s.Spec.Selector == nil {
		s.Spec.Selector = map[string]string{
			"app": "sadness",
		}
	}
	s.Spec.Ports = append(s.Spec.Ports,
		[]corev1.ServicePort{{
			Name:       servingv1.AutoscalingQueueMetricsPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       networking.AutoscalingQueueMetricsPort,
			TargetPort: intstr.FromString(servingv1.AutoscalingQueueMetricsPortName),
		}, {
			Name:       servingv1.UserQueueMetricsPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       networking.UserQueueMetricsPort,
			TargetPort: intstr.FromString(servingv1.UserQueueMetricsPortName),
		}, {
			Name:       servingv1.QueueAdminPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       networking.QueueAdminPort,
			TargetPort: intstr.FromInt(networking.QueueAdminPort),
		}, {
			Name:       pkgnet.ServicePortNameHTTP1 + "-istio",
			Protocol:   corev1.ProtocolTCP,
			Port:       networking.BackendHTTPPort,
			TargetPort: intstr.FromInt(networking.BackendHTTPPort),
		}}...)
}

func TestMakePublicService(t *testing.T) {
	tests := []struct {
		name string
		sks  *v1alpha1.ServerlessService
		want *corev1.Service
	}{{
		name: "HTTP - serve",
		sks:  sks(nil),
		want: svc(networking.ServiceTypePublic),
	}, {
		name: "HTTP - proxy",
		sks: sks(func(s *v1alpha1.ServerlessService) {
			s.Spec.Mode = v1alpha1.SKSOperationModeProxy
		}),
		want: svc(networking.ServiceTypePublic),
	}, {
		name: "HTTP2 - serve",
		sks: sks(func(s *v1alpha1.ServerlessService) {
			// Introduce some variability.
			s.UID = "1988"
			s.Annotations["cherub"] = "rock"
			s.Spec.ProtocolType = pkgnet.ProtocolH2C
		}),
		want: svc(networking.ServiceTypePublic, func(s *corev1.Service) {
			s.Spec.Ports = []corev1.ServicePort{{
				Name:       pkgnet.ServicePortNameH2C,
				Protocol:   corev1.ProtocolTCP,
				Port:       pkgnet.ServiceHTTP2Port,
				TargetPort: intstr.FromInt(networking.BackendHTTP2Port),
			}, {
				Name:       pkgnet.ServicePortNameHTTPS,
				Protocol:   corev1.ProtocolTCP,
				Port:       pkgnet.ServiceHTTPSPort,
				TargetPort: intstr.FromInt(networking.BackendHTTPSPort),
			}}
			s.Annotations = map[string]string{"cherub": "rock"}
			s.OwnerReferences[0].UID = "1988"
		}),
	}, {
		name: "HTTP2 -  serve - no backends",
		sks: sks(func(s *v1alpha1.ServerlessService) {
			s.Spec.ProtocolType = pkgnet.ProtocolH2C
		}),
		want: svc(networking.ServiceTypePublic, func(s *corev1.Service) {
			s.Spec.Ports = []corev1.ServicePort{{
				Name:       pkgnet.ServicePortNameH2C,
				Protocol:   corev1.ProtocolTCP,
				Port:       pkgnet.ServiceHTTP2Port,
				TargetPort: intstr.FromInt(networking.BackendHTTP2Port),
			}, {
				Name:       pkgnet.ServicePortNameHTTPS,
				Protocol:   corev1.ProtocolTCP,
				Port:       pkgnet.ServiceHTTPSPort,
				TargetPort: intstr.FromInt(networking.BackendHTTPSPort),
			}}
		}),
	}, {
		name: "HTTP2 - proxy",
		sks: sks(func(s *v1alpha1.ServerlessService) {
			s.Spec.ProtocolType = pkgnet.ProtocolH2C
			s.Spec.Mode = v1alpha1.SKSOperationModeProxy
			s.Labels["infinite"] = "sadness"
		}),
		want: svc(networking.ServiceTypePublic, func(s *corev1.Service) {
			s.Spec.Ports = []corev1.ServicePort{{
				Name:       pkgnet.ServicePortNameH2C,
				Protocol:   corev1.ProtocolTCP,
				Port:       pkgnet.ServiceHTTP2Port,
				TargetPort: intstr.FromInt(networking.BackendHTTP2Port),
			}, {
				Name:       pkgnet.ServicePortNameHTTPS,
				Protocol:   corev1.ProtocolTCP,
				Port:       pkgnet.ServiceHTTPSPort,
				TargetPort: intstr.FromInt(networking.BackendHTTPSPort),
			}}
			s.Labels["infinite"] = "sadness"
		}),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, want := MakePublicService(test.sks), test.want; !cmp.Equal(got, want, cmpopts.EquateEmpty()) {
				t.Errorf("Public K8s Service mismatch (-want, +got) = %v",
					cmp.Diff(want, got, cmpopts.EquateEmpty()))
			}
		})
	}
}

func TestMakeEndpoints(t *testing.T) {
	tests := []struct {
		name string
		sks  *v1alpha1.ServerlessService
		eps  *corev1.Endpoints
		want *corev1.Endpoints
	}{{
		name: "empty source",
		sks: sks(func(s *v1alpha1.ServerlessService) {
			s.Annotations["tonight"] = "tonight"
		}),
		eps: &corev1.Endpoints{},
		want: eps(func(e *corev1.Endpoints) {
			e.Annotations = map[string]string{"tonight": "tonight"}
		}),
	}, {
		name: "some endpoints, many ports",
		sks: sks(func(s *v1alpha1.ServerlessService) {
			s.Labels["ava"] = "adore"
		}),
		eps: &corev1.Endpoints{
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: "192.168.1.1",
				}, {
					IP: "10.5.6.21",
				}},
				Ports: []corev1.EndpointPort{{
					Name:     "http",
					Port:     8022,
					Protocol: "TCP",
				}, {
					Name:     "http",
					Port:     8012,
					Protocol: "TCP",
				}, {
					Name:     "https",
					Port:     8043,
					Protocol: "TCP",
				}},
			}},
		},
		want: eps(func(e *corev1.Endpoints) {
			e.Labels["ava"] = "adore"
			e.Subsets = []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: "192.168.1.1",
				}, {
					IP: "10.5.6.21",
				}},
				Ports: []corev1.EndpointPort{{
					Name:     "http",
					Port:     8012,
					Protocol: "TCP",
				}},
			}}
		}),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, want := MakePublicEndpoints(test.sks, test.eps), test.want; !cmp.Equal(got, want, cmpopts.EquateEmpty()) {
				t.Errorf("Public K8s Endpoints mismatch (-want, +got) = %v",
					cmp.Diff(want, got, cmpopts.EquateEmpty()))
			}
		})
	}
}

func TestFilterSubsetPorts(t *testing.T) {
	tests := []struct {
		name    string
		port    int32
		subsets []corev1.EndpointSubset
		want    []corev1.EndpointSubset
	}{{
		name: "nil",
		port: 1982,
	}, {
		name: "one port",
		port: 1984,
		subsets: []corev1.EndpointSubset{{
			Ports: []corev1.EndpointPort{{
				Name:     "http",
				Port:     1984,
				Protocol: "TCP",
			}},
		}},
		want: []corev1.EndpointSubset{{
			Ports: []corev1.EndpointPort{{
				Name:     "http",
				Port:     1984,
				Protocol: "TCP",
			}},
		}},
	}, {
		name: "two  ports, keep first",
		port: 1988,
		subsets: []corev1.EndpointSubset{{
			Ports: []corev1.EndpointPort{{
				Name:     "http",
				Port:     1988,
				Protocol: "TCP",
			}, {
				Name:     "http",
				Port:     1983,
				Protocol: "TCP",
			}},
		}},
		want: []corev1.EndpointSubset{{
			Ports: []corev1.EndpointPort{{
				Name:     "http",
				Port:     1988,
				Protocol: "TCP",
			}},
		}},
	}, {
		name: "three ports, keep middle",
		port: 2006,
		subsets: []corev1.EndpointSubset{{
			Ports: []corev1.EndpointPort{{
				Name:     "http",
				Port:     2009,
				Protocol: "TCP",
			}, {
				Name:     "http",
				Port:     2006,
				Protocol: "TCP",
			}, {
				Name:     "http",
				Port:     2019,
				Protocol: "TCP",
			}},
		}},
		want: []corev1.EndpointSubset{{
			Ports: []corev1.EndpointPort{{
				Name:     "http",
				Port:     2006,
				Protocol: "TCP",
			}},
		}},
	}, {
		name: "four ports including https ports, keep target and https port",
		port: 2006,
		subsets: []corev1.EndpointSubset{{
			Ports: []corev1.EndpointPort{{
				Name:     "http",
				Port:     2009,
				Protocol: "TCP",
			}, {
				Name:     "http",
				Port:     2006,
				Protocol: "TCP",
			}, {
				Name:     "http",
				Port:     2019,
				Protocol: "TCP",
			}, {
				Name:     "https",
				Port:     networking.BackendHTTPSPort,
				Protocol: "TCP",
			}},
		}},
		want: []corev1.EndpointSubset{{
			Ports: []corev1.EndpointPort{
				{
					Name:     "http",
					Port:     2006,
					Protocol: "TCP",
				},
				{
					Name:     "https",
					Port:     networking.BackendHTTPSPort,
					Protocol: "TCP",
				},
			},
		}},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, want := filterSubsetPorts(test.port, test.subsets), test.want; !cmp.Equal(got, want) {
				t.Errorf("Got = %v, want: %v, diff:\n%s", got, want, cmp.Diff(want, got))
			}
		})
	}
}

func TestMakePrivateService(t *testing.T) {
	tests := []struct {
		name     string
		sks      *v1alpha1.ServerlessService
		selector map[string]string
		want     *corev1.Service
	}{{
		name: "HTTP",
		sks:  sks(nil),
		selector: map[string]string{
			"app": "sadness",
		},
		want: svc(networking.ServiceTypePrivate, privateSvcMod),
	}, {
		name: "HTTP2 and long",
		sks: sks(func(s *v1alpha1.ServerlessService) {
			s.Name = "dream-tonight-cherub-rock-mayonnaise-hummer-disarm-rocket-soma-quiet"
			s.Spec.ProtocolType = pkgnet.ProtocolH2C
			s.Annotations["cherub"] = "rock"
			s.Labels["ava"] = "adore"
			s.UID = "1988"
		}),
		selector: map[string]string{
			"app": "today",
		},
		want: svc(networking.ServiceTypePrivate, func(s *corev1.Service) {
			// Set base name, that the private helper will tweak.
			s.Name = "dream-tonight-cherub-rock-mayonnaise-hummer-disarm-rocket-soma-quiet"
			s.OwnerReferences[0].UID = "1988"
			s.OwnerReferences[0].Name = "dream-tonight-cherub-rock-mayonnaise-hummer-disarm-rocket-soma-quiet"
			s.Spec.Selector = map[string]string{"app": "today"}
			s.Labels["ava"] = "adore"
			s.Labels[networking.SKSLabelKey] = "dream-tonight-cherub-rock-mayonnaise-hummer-disarm-rocket-soma-quiet"
			s.Annotations = map[string]string{"cherub": "rock"}
		}, privateSvcMod, func(s *corev1.Service) {
			// And now patch port to be http2.
			s.Spec.Ports[0] = corev1.ServicePort{
				Name:       pkgnet.ServicePortNameH2C,
				Protocol:   corev1.ProtocolTCP,
				Port:       pkgnet.ServiceHTTPPort,
				TargetPort: intstr.FromInt(networking.BackendHTTP2Port),
			}
			s.Spec.Ports[5] = corev1.ServicePort{
				Name:       pkgnet.ServicePortNameH2C + "-istio",
				Protocol:   corev1.ProtocolTCP,
				Port:       networking.BackendHTTP2Port,
				TargetPort: intstr.FromInt(networking.BackendHTTP2Port),
			}
		}),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, want := MakePrivateService(test.sks, test.selector), test.want; !cmp.Equal(got, want, cmpopts.EquateEmpty()) {
				t.Error("Private K8s Service mismatch (-want, +got) =", cmp.Diff(want, got, cmpopts.EquateEmpty()))
			}
		})
	}
}
