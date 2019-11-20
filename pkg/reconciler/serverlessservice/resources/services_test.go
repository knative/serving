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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	servingv1alpha1 "knative.dev/serving/pkg/apis/serving/v1alpha1"
)

var (
	goodPod = "good-pod"
	badPod  = "bad-pod"
)

// TODO(vagababov): Add templating here to get rid of the boilerplate.
func TestMakePublicService(t *testing.T) {
	tests := []struct {
		name string
		sks  *v1alpha1.ServerlessService
		want *corev1.Service
	}{{
		name: "HTTP - serve",
		sks: &v1alpha1.ServerlessService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "melon",
				Name:      "collie",
				UID:       "1982",
				// Those labels are propagated from the Revision->PA.
				Labels: map[string]string{
					serving.RevisionLabelKey: "collie",
					serving.RevisionUID:      "1982",
				},
			},
			Spec: v1alpha1.ServerlessServiceSpec{
				ProtocolType: networking.ProtocolHTTP1,
				Mode:         v1alpha1.SKSOperationModeServe,
			},
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "melon",
				Name:      "collie",
				Labels: map[string]string{
					// Those should be propagated.
					serving.RevisionLabelKey:  "collie",
					serving.RevisionUID:       "1982",
					networking.SKSLabelKey:    "collie",
					networking.ServiceTypeKey: "Public",
				},
				Annotations: map[string]string{},
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
					Name:       networking.ServicePortNameHTTP1,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.ServiceHTTPPort,
					TargetPort: intstr.FromInt(networking.BackendHTTPPort),
				}},
			},
		},
	}, {
		name: "HTTP - proxy",
		sks: &v1alpha1.ServerlessService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "melon",
				Name:      "collie",
				UID:       "1982",
				// Those labels are propagated from the Revision->PA.
				Labels: map[string]string{
					serving.RevisionLabelKey: "collie",
					serving.RevisionUID:      "1982",
				},
			},
			Spec: v1alpha1.ServerlessServiceSpec{
				Mode:         v1alpha1.SKSOperationModeProxy,
				ProtocolType: networking.ProtocolHTTP1,
			},
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "melon",
				Name:      "collie",
				Labels: map[string]string{
					// Those should be propagated.
					serving.RevisionLabelKey:  "collie",
					serving.RevisionUID:       "1982",
					networking.SKSLabelKey:    "collie",
					networking.ServiceTypeKey: "Public",
				},
				Annotations: map[string]string{},
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
					Name:       networking.ServicePortNameHTTP1,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.ServiceHTTPPort,
					TargetPort: intstr.FromInt(networking.BackendHTTPPort),
				}},
			},
		},
	}, {
		name: "HTTP2 -  serve",
		sks: &v1alpha1.ServerlessService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "siamese",
				Name:      "dream",
				UID:       "1988",
				// Those labels are propagated from the Revision->PA.
				Labels: map[string]string{
					serving.RevisionLabelKey: "dream",
					serving.RevisionUID:      "1988",
				},
				Annotations: map[string]string{
					"cherub": "rock",
				},
			},
			Spec: v1alpha1.ServerlessServiceSpec{
				ProtocolType: networking.ProtocolH2C,
				Mode:         v1alpha1.SKSOperationModeServe,
			},
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "siamese",
				Name:      "dream",
				Labels: map[string]string{
					// Those should be propagated.
					serving.RevisionLabelKey:  "dream",
					serving.RevisionUID:       "1988",
					networking.SKSLabelKey:    "dream",
					networking.ServiceTypeKey: "Public",
				},
				Annotations: map[string]string{
					"cherub": "rock",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "ServerlessService",
					Name:               "dream",
					UID:                "1988",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name:       networking.ServicePortNameH2C,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.ServiceHTTP2Port,
					TargetPort: intstr.FromInt(networking.BackendHTTP2Port),
				}},
			},
		},
	}, {
		name: "HTTP2 -  serve - no backends",
		sks: &v1alpha1.ServerlessService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "siamese",
				Name:      "dream",
				UID:       "1988",
				// Those labels are propagated from the Revision->PA.
				Labels: map[string]string{
					serving.RevisionLabelKey: "dream",
					serving.RevisionUID:      "1988",
				},
				Annotations: map[string]string{
					"cherub": "rock",
				},
			},
			Spec: v1alpha1.ServerlessServiceSpec{
				ProtocolType: networking.ProtocolH2C,
				Mode:         v1alpha1.SKSOperationModeServe,
			},
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "siamese",
				Name:      "dream",
				Labels: map[string]string{
					// Those should be propagated.
					serving.RevisionLabelKey:  "dream",
					serving.RevisionUID:       "1988",
					networking.SKSLabelKey:    "dream",
					networking.ServiceTypeKey: "Public",
				},
				Annotations: map[string]string{
					"cherub": "rock",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "ServerlessService",
					Name:               "dream",
					UID:                "1988",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name:       networking.ServicePortNameH2C,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.ServiceHTTP2Port,
					TargetPort: intstr.FromInt(networking.BackendHTTP2Port),
				}},
			},
		},
	}, {
		name: "HTTP2 - proxy",
		sks: &v1alpha1.ServerlessService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "siamese",
				Name:      "dream",
				UID:       "1988",
				// Those labels are propagated from the Revision->PA.
				Labels: map[string]string{
					serving.RevisionLabelKey: "dream",
					serving.RevisionUID:      "1988",
				},
				Annotations: map[string]string{
					"cherub": "rock",
				},
			},
			Spec: v1alpha1.ServerlessServiceSpec{
				ProtocolType: networking.ProtocolH2C,
				Mode:         v1alpha1.SKSOperationModeProxy,
			},
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "siamese",
				Name:      "dream",
				Labels: map[string]string{
					// Those should be propagated.
					serving.RevisionLabelKey:  "dream",
					serving.RevisionUID:       "1988",
					networking.SKSLabelKey:    "dream",
					networking.ServiceTypeKey: "Public",
				},
				Annotations: map[string]string{
					"cherub": "rock",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "ServerlessService",
					Name:               "dream",
					UID:                "1988",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name:       networking.ServicePortNameH2C,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.ServiceHTTP2Port,
					TargetPort: intstr.FromInt(networking.BackendHTTP2Port),
				}},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakePublicService(test.sks)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Public K8s Service mismatch (-want, +got) = %v", diff)
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
		sks: &v1alpha1.ServerlessService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "melon",
				Name:      "collie",
				UID:       "1982",
				// Those labels are propagated from the Revision->PA.
				Labels: map[string]string{
					serving.RevisionLabelKey: "collie",
					serving.RevisionUID:      "1982",
				},
				Annotations: map[string]string{
					"cherub": "rock",
				},
			},
			Spec: v1alpha1.ServerlessServiceSpec{
				ProtocolType: networking.ProtocolHTTP1,
				Mode:         v1alpha1.SKSOperationModeServe,
			},
		},
		eps: &corev1.Endpoints{},
		want: &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "melon",
				Name:      "collie",
				Labels: map[string]string{
					serving.RevisionLabelKey:  "collie",
					serving.RevisionUID:       "1982",
					networking.SKSLabelKey:    "collie",
					networking.ServiceTypeKey: "Public",
				},
				Annotations: map[string]string{
					"cherub": "rock",
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
		},
	}, {
		name: "some endpoints, many ports",
		sks: &v1alpha1.ServerlessService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "melon",
				Name:      "collie",
				UID:       "1982",
				// Those labels are propagated from the Revision->PA.
				Labels: map[string]string{
					serving.RevisionLabelKey:  "collie",
					serving.RevisionUID:       "1982",
					networking.ServiceTypeKey: "Public",
				},
				Annotations: map[string]string{
					"cherub": "rock",
				},
			},
			Spec: v1alpha1.ServerlessServiceSpec{
				ProtocolType: networking.ProtocolHTTP1,
			},
		},
		eps: &corev1.Endpoints{
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP:       "192.168.1.1",
					NodeName: &goodPod,
				}, {
					IP:       "10.5.6.21",
					NodeName: &badPod,
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
		want: &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "melon",
				Name:      "collie",
				Labels: map[string]string{
					serving.RevisionLabelKey:  "collie",
					serving.RevisionUID:       "1982",
					networking.SKSLabelKey:    "collie",
					networking.ServiceTypeKey: "Public",
				},
				Annotations: map[string]string{
					"cherub": "rock",
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
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP:       "192.168.1.1",
					NodeName: &goodPod,
				}, {
					IP:       "10.5.6.21",
					NodeName: &badPod,
				}},
				Ports: []corev1.EndpointPort{{
					Name:     "http",
					Port:     8012,
					Protocol: "TCP",
				}},
			}},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakePublicEndpoints(test.sks, test.eps)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Public K8s Endpoints mismatch (-want, +got) = %v", diff)
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
		sks: &v1alpha1.ServerlessService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "melon",
				Name:      "collie",
				UID:       "1982",
				// Those labels are propagated from the Revision->PA.
				Labels: map[string]string{
					serving.RevisionLabelKey: "collie",
					serving.RevisionUID:      "1982",
				},
			},
			Spec: v1alpha1.ServerlessServiceSpec{
				ProtocolType: networking.ProtocolHTTP1,
				// To make sure this does not affect private service in any way.
				Mode: v1alpha1.SKSOperationModeProxy,
			},
		},
		selector: map[string]string{
			"app": "sadness",
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "melon",
				Name:      "collie-private",
				Labels: map[string]string{
					// Those should be propagated.
					serving.RevisionLabelKey:  "collie",
					serving.RevisionUID:       "1982",
					networking.SKSLabelKey:    "collie",
					networking.ServiceTypeKey: "Private",
				},
				Annotations: map[string]string{},
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
				Selector: map[string]string{
					"app": "sadness",
				},
				Ports: []corev1.ServicePort{{
					Name:       networking.ServicePortNameHTTP1,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.ServiceHTTPPort,
					TargetPort: intstr.FromInt(networking.BackendHTTPPort),
				}, {
					Name:       servingv1alpha1.AutoscalingQueueMetricsPortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.AutoscalingQueueMetricsPort,
					TargetPort: intstr.FromString(servingv1alpha1.AutoscalingQueueMetricsPortName),
				}, {
					Name:       servingv1alpha1.UserQueueMetricsPortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.UserQueueMetricsPort,
					TargetPort: intstr.FromString(servingv1alpha1.UserQueueMetricsPortName),
				}, {
					Name:       servingv1alpha1.QueueAdminPortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.QueueAdminPort,
					TargetPort: intstr.FromInt(networking.QueueAdminPort),
				}},
			},
		},
	}, {
		name: "HTTP2 and long",
		sks: &v1alpha1.ServerlessService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "siamese",
				Name:      "dream-tonight-cherub-rock-mayonaise-hummer-disarm-rocket-soma-quiet",
				UID:       "1988",
				// Those labels are propagated from the Revision->PA.
				Labels: map[string]string{
					serving.RevisionLabelKey: "dream",
					serving.RevisionUID:      "1988",
				},
				Annotations: map[string]string{
					"cherub": "rock",
				},
			},
			Spec: v1alpha1.ServerlessServiceSpec{
				ProtocolType: networking.ProtocolH2C,
			},
		},
		selector: map[string]string{
			"app": "today",
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "siamese",
				Name:      "dream-tonight-cherub-ro9598b55360c44122a4442ce54caa8619-private",
				Labels: map[string]string{
					// Those should be propagated.
					serving.RevisionLabelKey:  "dream",
					serving.RevisionUID:       "1988",
					networking.SKSLabelKey:    "dream-tonight-cherub-rock-mayonaise-hummer-disarm-rocket-soma-quiet",
					networking.ServiceTypeKey: "Private",
				},
				Annotations: map[string]string{
					"cherub": "rock",
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "ServerlessService",
					Name:               "dream-tonight-cherub-rock-mayonaise-hummer-disarm-rocket-soma-quiet",
					UID:                "1988",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app": "today",
				},
				Ports: []corev1.ServicePort{{
					Name:       networking.ServicePortNameH2C,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.ServiceHTTPPort,
					TargetPort: intstr.FromInt(networking.BackendHTTP2Port),
				}, {
					Name:       servingv1alpha1.AutoscalingQueueMetricsPortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.AutoscalingQueueMetricsPort,
					TargetPort: intstr.FromString(servingv1alpha1.AutoscalingQueueMetricsPortName),
				}, {
					Name:       servingv1alpha1.UserQueueMetricsPortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.UserQueueMetricsPort,
					TargetPort: intstr.FromString(servingv1alpha1.UserQueueMetricsPortName),
				}, {
					Name:       servingv1alpha1.QueueAdminPortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.QueueAdminPort,
					TargetPort: intstr.FromInt(networking.QueueAdminPort),
				}},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakePrivateService(test.sks, test.selector)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("Private K8s Service mismatch (-want, +got) = %v", diff)
			}
		})
	}
}
