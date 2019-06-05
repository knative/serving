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

	"github.com/knative/pkg/ptr"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
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
				// Those labels are propagated from the Revision->KPA.
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
				// Those labels are propagated from the Revision->KPA.
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
				// Those labels are propagated from the Revision->KPA.
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
				// Those labels are propagated from the Revision->KPA.
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
				// Those labels are propagated from the Revision->KPA.
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
				// Those labels are propagated from the Revision->KPA.
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
		name: "some endpoints",
		sks: &v1alpha1.ServerlessService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "melon",
				Name:      "collie",
				UID:       "1982",
				// Those labels are propagated from the Revision->KPA.
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
					Port:     8012,
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
				// Those labels are propagated from the Revision->KPA.
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
				Namespace:    "melon",
				GenerateName: "collie-",
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
				}},
			},
		},
	}, {
		name: "HTTP2",
		sks: &v1alpha1.ServerlessService{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "siamese",
				Name:      "dream",
				UID:       "1988",
				// Those labels are propagated from the Revision->KPA.
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
				Namespace:    "siamese",
				GenerateName: "dream-",
				Labels: map[string]string{
					// Those should be propagated.
					serving.RevisionLabelKey:  "dream",
					serving.RevisionUID:       "1988",
					networking.SKSLabelKey:    "dream",
					networking.ServiceTypeKey: "Private",
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
				Selector: map[string]string{
					"app": "today",
				},
				Ports: []corev1.ServicePort{{
					Name:       networking.ServicePortNameH2C,
					Protocol:   corev1.ProtocolTCP,
					Port:       networking.ServiceHTTPPort,
					TargetPort: intstr.FromInt(networking.BackendHTTP2Port),
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
