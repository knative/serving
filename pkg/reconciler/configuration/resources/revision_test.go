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

package resources

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

var fakeCurTime = time.Unix(1e9, 20102021)

func TestMakeRevisions(t *testing.T) {
	tm := fakeCurTime

	tests := []struct {
		name          string
		responsiveGC  bool
		configuration *v1.Configuration
		want          *v1.Revision
	}{{
		name:         "no build",
		responsiveGC: true,
		configuration: &v1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:  "no",
				Name:       "build",
				Generation: 10,
				Labels: map[string]string{
					serving.ServiceUIDLabelKey: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
				},
				UID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			},
			Spec: v1.ConfigurationSpec{
				Template: v1.RevisionTemplateSpec{
					Spec: v1.RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
					},
				},
			},
		},
		want: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "no",
				Name:      "build-00010",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "build",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
					UID:                "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
				}},
				Labels: map[string]string{
					serving.ConfigurationLabelKey:           "build",
					serving.ConfigurationGenerationLabelKey: "10",
					serving.ConfigurationUIDLabelKey:        "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
					serving.RoutingStateLabelKey:            "pending",
					serving.ServiceLabelKey:                 "",
					serving.ServiceUIDLabelKey:              "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
				},
				Annotations: map[string]string{
					serving.RoutingStateModifiedAnnotationKey: v1.RoutingStateModifiedString(fakeCurTime),
				},
			},
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				},
			},
		},
	}, {
		name: "with labels",
		configuration: &v1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:  "with",
				Name:       "labels",
				Generation: 100,
				Labels: map[string]string{
					serving.ServiceUIDLabelKey: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
				},
				UID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			},
			Spec: v1.ConfigurationSpec{
				Template: v1.RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"foo": "bar",
							"baz": "blah",
						},
					},
					Spec: v1.RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
					},
				},
			},
		},
		want: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "with",
				Name:      "labels-00100",
				Annotations: map[string]string{
					serving.RoutingStateModifiedAnnotationKey: v1.RoutingStateModifiedString(fakeCurTime),
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "labels",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
					UID:                "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
				}},
				Labels: map[string]string{
					serving.ConfigurationLabelKey:           "labels",
					serving.ConfigurationGenerationLabelKey: "100",
					serving.ConfigurationUIDLabelKey:        "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
					serving.RoutingStateLabelKey:            "pending",
					serving.ServiceLabelKey:                 "",
					serving.ServiceUIDLabelKey:              "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
					"foo":                                   "bar",
					"baz":                                   "blah",
				},
			},
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				},
			},
		},
	}, {
		name: "with annotations",
		configuration: &v1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:  "with",
				Name:       "annotations",
				Generation: 100,
				Labels: map[string]string{
					serving.ServiceUIDLabelKey: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
				},
				UID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			},
			Spec: v1.ConfigurationSpec{
				Template: v1.RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"foo": "bar",
							"baz": "blah",
						},
					},
					Spec: v1.RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
					},
				},
			},
		},
		want: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "with",
				Name:      "annotations-00100",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "annotations",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
					UID:                "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
				}},
				Labels: map[string]string{
					serving.ConfigurationLabelKey:           "annotations",
					serving.ConfigurationGenerationLabelKey: "100",
					serving.ConfigurationUIDLabelKey:        "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
					serving.RoutingStateLabelKey:            "pending",
					serving.ServiceLabelKey:                 "",
					serving.ServiceUIDLabelKey:              "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
				},
				Annotations: map[string]string{
					"foo": "bar",
					"baz": "blah",
					serving.RoutingStateModifiedAnnotationKey: v1.RoutingStateModifiedString(fakeCurTime),
				},
			},
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				},
			},
		},
	}, {
		name:         "with creator annotation from config",
		responsiveGC: true,
		configuration: &v1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "anno",
				Name:      "config",
				Annotations: map[string]string{
					"serving.knative.dev/creator":      "admin",
					"serving.knative.dev/lastModifier": "someone",
					serving.RoutesAnnotationKey:        "route",
				},
				Labels: map[string]string{
					serving.ServiceUIDLabelKey: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
				},
				Generation: 10,
				UID:        "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			},
			Spec: v1.ConfigurationSpec{
				Template: v1.RevisionTemplateSpec{
					Spec: v1.RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
					},
				},
			},
		},
		want: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "anno",
				Name:      "config-00010",
				Annotations: map[string]string{
					"serving.knative.dev/creator":             "someone",
					serving.RoutesAnnotationKey:               "route",
					serving.RoutingStateModifiedAnnotationKey: v1.RoutingStateModifiedString(fakeCurTime),
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "config",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
					UID:                "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
				}},
				Labels: map[string]string{
					serving.ConfigurationLabelKey:           "config",
					serving.ConfigurationGenerationLabelKey: "10",
					serving.ConfigurationUIDLabelKey:        "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
					serving.RoutingStateLabelKey:            "active",
					serving.ServiceLabelKey:                 "",
					serving.ServiceUIDLabelKey:              "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
				},
			},
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				},
			},
		},
	}, {
		name: "with creator annotation from config with other annotations",
		configuration: &v1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "anno",
				Name:      "config",
				Annotations: map[string]string{
					"serving.knative.dev/creator":      "admin",
					"serving.knative.dev/lastModifier": "someone",
				},
				Labels: map[string]string{
					serving.ServiceUIDLabelKey: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
				},
				Generation: 10,
				UID:        "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			},
			Spec: v1.ConfigurationSpec{
				Template: v1.RevisionTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"foo": "bar",
							"baz": "blah",
						},
					},
					Spec: v1.RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
					},
				},
			},
		},
		want: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "anno",
				Name:      "config-00010",
				Annotations: map[string]string{
					"serving.knative.dev/creator": "someone",
					"foo":                         "bar",
					"baz":                         "blah",
					serving.RoutingStateModifiedAnnotationKey: v1.RoutingStateModifiedString(fakeCurTime),
				},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "config",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
					UID:                "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
				}},
				Labels: map[string]string{
					serving.ConfigurationLabelKey:           "config",
					serving.ConfigurationGenerationLabelKey: "10",
					serving.ConfigurationUIDLabelKey:        "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
					serving.RoutingStateLabelKey:            "pending",
					serving.ServiceLabelKey:                 "",
					serving.ServiceUIDLabelKey:              "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
				},
			},
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakeRevision(context.Background(), test.configuration, tm)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Error("MakeRevision (-want, +got) =", diff)
			}
		})
	}
}
