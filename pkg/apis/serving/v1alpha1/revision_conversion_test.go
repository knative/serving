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

package v1alpha1

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/apis"
	duckv1beta1 "github.com/knative/pkg/apis/duck/v1beta1"
	"github.com/knative/pkg/ptr"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
)

func TestRevisionConversionBadType(t *testing.T) {
	good, bad := &Revision{}, &Service{}

	if err := good.ConvertUp(context.Background(), bad); err == nil {
		t.Errorf("ConvertUp() = %#v, wanted error", bad)
	}

	if err := good.ConvertDown(context.Background(), bad); err == nil {
		t.Errorf("ConvertDown() = %#v, wanted error", good)
	}
}

func TestRevisionConversion(t *testing.T) {
	tests := []struct {
		name     string
		in       *Revision
		badField string
	}{{
		name: "good roundtrip w/ lots of parts",
		in: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					PodSpec: v1beta1.PodSpec{
						ServiceAccountName: "robocop",
						Containers: []corev1.Container{{
							Image: "busybox",
							VolumeMounts: []corev1.VolumeMount{{
								MountPath: "/mount/path",
								Name:      "the-name",
								ReadOnly:  true,
							}},
						}},
						Volumes: []corev1.Volume{{
							Name: "the-name",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "foo",
								},
							},
						}},
					},
					TimeoutSeconds:       ptr.Int64(18),
					ContainerConcurrency: 53,
				},
			},
			Status: RevisionStatus{
				Status: duckv1beta1.Status{
					ObservedGeneration: 1,
					Conditions: duckv1beta1.Conditions{{
						Type:   "Ready",
						Status: "True",
					}},
				},
				ServiceName: "foo-bar",
				LogURL:      "http://logger.io",
			},
		},
	}, {
		name:     "bad roundtrip w/ build ref",
		badField: "buildRef",
		in: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: RevisionSpec{
				DeprecatedBuildRef: &corev1.ObjectReference{
					APIVersion: "build.knative.dev/v1alpha1",
					Kind:       "Build",
					Name:       "foo",
				},
				RevisionSpec: v1beta1.RevisionSpec{
					PodSpec: v1beta1.PodSpec{
						ServiceAccountName: "robocop",
						Containers: []corev1.Container{{
							Image: "busybox",
							VolumeMounts: []corev1.VolumeMount{{
								MountPath: "/mount/path",
								Name:      "the-name",
								ReadOnly:  true,
							}},
						}},
						Volumes: []corev1.Volume{{
							Name: "the-name",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "foo",
								},
							},
						}},
					},
					TimeoutSeconds:       ptr.Int64(18),
					ContainerConcurrency: 53,
				},
			},
			Status: RevisionStatus{
				Status: duckv1beta1.Status{
					ObservedGeneration: 1,
					Conditions: duckv1beta1.Conditions{{
						Type:   "Ready",
						Status: "True",
					}},
				},
			},
		},
	}}

	toDeprecated := func(in *Revision) *Revision {
		out := in.DeepCopy()
		out.Spec.DeprecatedContainer = &out.Spec.Containers[0]
		out.Spec.Containers = nil
		return out
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beta := &v1beta1.Revision{}
			if err := test.in.ConvertUp(context.Background(), beta); err != nil {
				if test.badField != "" {
					cce, ok := err.(*CannotConvertError)
					if ok && cce.Field == test.badField {
						return
					}
				}
				t.Errorf("ConvertUp() = %v", err)
			} else if test.badField != "" {
				t.Errorf("CovnertUp() = %#v, wanted bad field %q", beta,
					test.badField)
				return
			}
			got := &Revision{}
			if err := got.ConvertDown(context.Background(), beta); err != nil {
				t.Errorf("ConvertDown() = %v", err)
			}
			if diff := cmp.Diff(test.in, got); diff != "" {
				t.Errorf("roundtrip (-want, +got) = %v", diff)
			}
		})

		// A variant of the test that uses `container:`,
		// but end up with what we have above anyways.
		t.Run(test.name+" (deprecated)", func(t *testing.T) {
			start := toDeprecated(test.in)
			beta := &v1beta1.Revision{}
			if err := start.ConvertUp(context.Background(), beta); err != nil {
				if test.badField != "" {
					cce, ok := err.(*CannotConvertError)
					if ok && cce.Field == test.badField {
						return
					}
				}
				t.Errorf("ConvertUp() = %v", err)
			} else if test.badField != "" {
				t.Errorf("CovnertUp() = %#v, wanted bad field %q", beta,
					test.badField)
				return
			}
			got := &Revision{}
			if err := got.ConvertDown(context.Background(), beta); err != nil {
				t.Errorf("ConvertDown() = %v", err)
			}
			if diff := cmp.Diff(test.in, got); diff != "" {
				t.Errorf("roundtrip (-want, +got) = %v", diff)
			}
		})
	}
}

func TestRevisionConversionError(t *testing.T) {
	tests := []struct {
		name string
		in   *Revision
		want *apis.FieldError
	}{{
		name: "multiple containers in podspec",
		in: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					PodSpec: v1beta1.PodSpec{
						ServiceAccountName: "robocop",
						Containers: []corev1.Container{{
							Image: "busybox",
						}, {
							Image: "helloworld",
						}},
					},
					TimeoutSeconds:       ptr.Int64(18),
					ContainerConcurrency: 53,
				},
			},
			Status: RevisionStatus{
				Status: duckv1beta1.Status{
					ObservedGeneration: 1,
					Conditions: duckv1beta1.Conditions{{
						Type:   "Ready",
						Status: "True",
					}},
				},
				ServiceName: "foo-bar",
				LogURL:      "http://logger.io",
			},
		},
		want: apis.ErrMultipleOneOf("containers"),
	}, {
		name: "no containers in podspec",
		in: &Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					PodSpec: v1beta1.PodSpec{
						ServiceAccountName: "robocop",
						Containers:         []corev1.Container{},
					},
					TimeoutSeconds:       ptr.Int64(18),
					ContainerConcurrency: 53,
				},
			},
			Status: RevisionStatus{
				Status: duckv1beta1.Status{
					ObservedGeneration: 1,
					Conditions: duckv1beta1.Conditions{{
						Type:   "Ready",
						Status: "True",
					}},
				},
				ServiceName: "foo-bar",
				LogURL:      "http://logger.io",
			},
		},
		want: apis.ErrMissingOneOf("container", "containers"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beta := &v1beta1.Revision{}
			got := test.in.ConvertUp(context.Background(), beta)
			if got == nil {
				t.Errorf("ConvertUp() = %#v, wanted %v", beta, test.want)
			}
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("roundtrip (-want, +got) = %v", diff)
			}
		})
	}
}
