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

package v1alpha1

import (
	"context"
	"strings"
	"testing"

	"knative.dev/serving/pkg/apis/config"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

const incorrectDNS1035Label = "not a DNS 1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]"

func TestServiceValidation(t *testing.T) {
	tests := []struct {
		name string
		s    *Service
		wc   func(context.Context) context.Context
		want *apis.FieldError
	}{{
		name: "valid runLatest",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1.RevisionSpec{
									PodSpec: corev1.PodSpec{
										Containers: []corev1.Container{{
											Image: "hellworld",
										}},
									},
								},
							},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "invalid runLatest (has spec.generation)",
		wc:   apis.DisallowDeprecated,
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedGeneration: 12,
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1.RevisionSpec{
									PodSpec: corev1.PodSpec{
										Containers: []corev1.Container{{
											Image: "hellworld",
										}},
									},
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.generation", "spec.runLatest",
			"spec.runLatest.configuration.revisionTemplate"),
	}, {
		name: "valid pinned",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedPinned: &PinnedType{
					RevisionName: "asdf",
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "valid pinned (deprecated disallowed)",
		wc:   apis.DisallowDeprecated,
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedPinned: &PinnedType{
					RevisionName: "asdf",
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1.RevisionSpec{
									PodSpec: corev1.PodSpec{
										Containers: []corev1.Container{{
											Image: "hellworld",
										}},
									},
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.pinned",
			"spec.pinned.configuration.revisionTemplate"),
	}, {
		name: "valid release -- one revision",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions: []string{"asdf"},
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "valid release -- two revisions",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions:      []string{"asdf", "fdsa"},
					RolloutPercent: 42,
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "valid manual",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedManual: &ManualType{},
			},
		},
		want: apis.ErrDisallowedFields("spec.manual"),
	}, {
		name: "invalid multiple types",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
				DeprecatedPinned: &PinnedType{
					RevisionName: "asdf",
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: &apis.FieldError{
			Message: "expected exactly one, got both",
			Paths:   []string{"spec.pinned", "spec.runLatest"},
		},
	}, {
		name: "invalid missing type",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
		},
		want: &apis.FieldError{
			Message: "expected exactly one, got neither",
			Paths: []string{"spec.pinned", "spec.release",
				"spec.template", "spec.runLatest"},
		},
	}, {
		name: "invalid runLatest",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Name:      "foo",
									Image:     "hellworld",
									Lifecycle: &corev1.Lifecycle{},
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.runLatest.configuration.revisionTemplate.spec.container.lifecycle"),
	}, {
		name: "invalid pinned",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedPinned: &PinnedType{
					RevisionName: "asdf",
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Name:      "foo",
									Image:     "hellworld",
									Lifecycle: &corev1.Lifecycle{},
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.pinned.configuration.revisionTemplate.spec.container.lifecycle"),
	}, {
		name: "invalid release -- too few revisions; nil",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrMissingField("spec.release.revisions"),
	}, {
		name: "invalid release -- revision name invalid, long",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions: []string{strings.Repeat("a", 64)},
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrInvalidValue("not a DNS 1035 label: [must be no more than 63 characters]", "spec.release.revisions[0]"),
	}, {
		name: "invalid release -- revision name invalid, incorrect",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions: []string{".negative"},
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrInvalidValue(incorrectDNS1035Label, "spec.release.revisions[0]"),
	}, {
		name: "valid release -- with @latest",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions: []string{"s-1-00001", ReleaseLatestRevisionKeyword},
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "invalid release -- too few revisions; empty slice",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions: []string{},
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrMissingField("spec.release.revisions"),
	}, {
		name: "invalid release -- too many revisions",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions: []string{"asdf", "fdsa", "abcde"},
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrOutOfBoundsValue(3, 1, 2, "spec.release.revisions"),
	}, {
		name: "invalid release -- rollout greater than 99",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions:      []string{"asdf", "fdsa"},
					RolloutPercent: 100,
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrOutOfBoundsValue(100, 0, 99, "spec.release.rolloutPercent"),
	}, {
		name: "invalid release -- rollout less than 0",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions:      []string{"asdf", "fdsa"},
					RolloutPercent: -50,
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrOutOfBoundsValue(-50, 0, 99, "spec.release.rolloutPercent"),
	}, {
		name: "invalid release -- non-zero rollout for single revision",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions:      []string{"asdf"},
					RolloutPercent: 10,
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: apis.ErrInvalidValue(10, "spec.release.rolloutPercent"),
	}, {
		name: "invalid name - dots",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "do.not.use.dots",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: &apis.FieldError{
			Message: incorrectDNS1035Label,
			Paths:   []string{"metadata.name"},
		},
	}, {
		name: "invalid name - too long",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("a", 64),
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
			},
		},
		want: &apis.FieldError{
			Message: "not a DNS 1035 label: [must be no more than 63 characters]",
			Paths:   []string{"metadata.name"},
		},
	}, {
		name: "runLatest with traffic",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "invalid",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "hellworld",
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        ptr.Int64(100),
						},
					}},
				},
			},
		},
		want: apis.ErrMultipleOneOf("spec.runLatest", "spec.traffic"),
	}, {
		name: "valid v1beta1 subset (pinned)",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Image: "helloworld",
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							RevisionName: "valid-00001",
							Percent:      ptr.Int64(100),
						},
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "invalid v1beta1 subset (deprecated field within inline spec)",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							DeprecatedConcurrencyModel: "Multi",
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Image: "helloworld",
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							RevisionName: "valid-00001",
							Percent:      ptr.Int64(100),
						},
					}},
				},
			},
		},
		want: apis.ErrDisallowedFields("spec.template.spec.concurrencyModel"),
	}, {
		name: "valid v1beta1 subset (run latest)",
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Image: "helloworld",
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							LatestRevision: ptr.Bool(true),
							Percent:        ptr.Int64(100),
						},
					}},
				},
			},
		},
		want: nil,
	}, {
		name: "valid inline",
		// Should not affect anything.
		wc: apis.DisallowDeprecated,
		s: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Image: "hellworld",
									}},
								},
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							Percent: ptr.Int64(100),
						},
					}},
				},
			},
		},
		want: nil,
	}}

	// TODO(mattmoor): Add a test for default configurationName

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.wc != nil {
				ctx = test.wc(ctx)
			}
			got := test.s.Validate(ctx)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate() (-want, +got) = %v", diff)
			}
		})
	}
}

func TestRunLatestTypeValidation(t *testing.T) {
	tests := []struct {
		name string
		rlt  *RunLatestType
		want *apis.FieldError
	}{{
		name: "valid",
		rlt: &RunLatestType{
			Configuration: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "hellworld",
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "propagate revision failures",
		rlt: &RunLatestType{
			Configuration: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Name:      "stuart",
							Image:     "hellworld",
							Lifecycle: &corev1.Lifecycle{},
						},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("configuration.revisionTemplate.spec.container.lifecycle"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.rlt.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("validateContainer (-want, +got) = %v", diff)
			}
		})
	}
}

func TestPinnedTypeValidation(t *testing.T) {
	tests := []struct {
		name string
		pt   *PinnedType
		want *apis.FieldError
	}{{
		name: "valid",
		pt: &PinnedType{
			RevisionName: "foo",
			Configuration: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "hellworld",
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "missing revision name",
		pt: &PinnedType{
			Configuration: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Image: "hellworld",
						},
					},
				},
			},
		},
		want: apis.ErrMissingField("revisionName"),
	}, {
		name: "propagate revision failures",
		pt: &PinnedType{
			RevisionName: "foo",
			Configuration: ConfigurationSpec{
				DeprecatedRevisionTemplate: &RevisionTemplateSpec{
					Spec: RevisionSpec{
						DeprecatedContainer: &corev1.Container{
							Name:      "stuart",
							Image:     "hellworld",
							Lifecycle: &corev1.Lifecycle{},
						},
					},
				},
			},
		},
		want: apis.ErrDisallowedFields("configuration.revisionTemplate.spec.container.lifecycle"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.pt.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("validateContainer (-want, +got) = %v", diff)
			}
		})
	}
}

func TestImmutableServiceFields(t *testing.T) {
	tests := []struct {
		name string
		new  *Service
		old  *Service
		want *apis.FieldError
	}{{
		name: "without byo-name",
		new: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "no-byo-name",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld:foo",
								},
							},
						},
					},
				},
			},
		},
		old: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "no-byo-name",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld:bar",
								},
							},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "good byo-name (name change)",
		new: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Name: "byo-name-foo",
							},
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld:foo",
								},
							},
						},
					},
				},
			},
		},
		old: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Name: "byo-name-bar",
							},
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld:bar",
								},
							},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "good byo-name (mode change, no delta)",
		new: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Name: "byo-name-foo",
							},
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld:foo",
								},
							},
						},
					},
				},
			},
		},
		old: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions: []string{"foo"},
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Name: "byo-name-foo",
							},
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld:foo",
								},
							},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "good byo-name (mode change, with delta)",
		new: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ServiceSpec{
				DeprecatedPinned: &PinnedType{
					RevisionName: "bar",
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Name: "byo-name-foo",
							},
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld:foo",
								},
							},
						},
					},
				},
			},
		},
		old: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: "byo-name-bar",
						},
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Image: "helloworld:bar",
									}},
								},
							},
						},
					},
				},
			},
		},
		want: nil,
	}, {
		name: "good byo-name (mode change to manual)",
		new: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ServiceSpec{
				DeprecatedPinned: &PinnedType{
					RevisionName: "bar",
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Name: "byo-name-foo",
							},
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld:bar",
								},
							},
						},
					},
				},
			},
		},
		old: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ServiceSpec{
				DeprecatedManual: &ManualType{},
			},
		},
		want: nil,
	}, {
		name: "bad byo-name (mode change, with delta)",
		new: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Name: "byo-name-foo",
							},
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld:foo",
								},
							},
						},
					},
				},
			},
		},
		old: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "byo-name",
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions: []string{"foo"},
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Name: "byo-name-foo",
							},
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld:bar",
								},
							},
						},
					},
				},
			},
		},
		want: &apis.FieldError{
			Message: "Saw the following changes without a name change (-old +new)",
			Paths:   []string{"spec.runLatest.configuration.revisionTemplate"},
			Details: "{*v1alpha1.RevisionTemplateSpec}.Spec.DeprecatedContainer.Image:\n\t-: \"helloworld:bar\"\n\t+: \"helloworld:foo\"\n",
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinUpdate(ctx, test.old)
			got := test.new.Validate(ctx)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func TestServiceSubresourceUpdate(t *testing.T) {
	tests := []struct {
		name        string
		service     *Service
		subresource string
		want        *apis.FieldError
	}{{
		name: "status update with valid revision template",
		service: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld",
								},
								RevisionSpec: v1.RevisionSpec{
									TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds - 1),
								},
							},
						},
					},
				},
			},
		},
		subresource: "status",
		want:        nil,
	}, {
		name: "status update with invalid revision template",
		service: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld",
								},
								RevisionSpec: v1.RevisionSpec{
									TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds + 1),
								},
							},
						},
					},
				},
			},
		},
		subresource: "status",
		want:        nil,
	}, {
		name: "non-status sub resource update with valid revision template",
		service: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld",
								},
								RevisionSpec: v1.RevisionSpec{
									TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds - 1),
								},
							},
						},
					},
				},
			},
		},
		subresource: "foo",
		want:        nil,
	}, {
		name: "non-status sub resource update with invalid revision template",
		service: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								DeprecatedContainer: &corev1.Container{
									Image: "helloworld",
								},
								RevisionSpec: v1.RevisionSpec{
									TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds + 1),
								},
							},
						},
					},
				},
			},
		},
		subresource: "foo",
		want: apis.ErrOutOfBoundsValue(config.DefaultMaxRevisionTimeoutSeconds+1, 0,
			config.DefaultMaxRevisionTimeoutSeconds,
			"spec.runLatest.configuration.revisionTemplate.spec.timeoutSeconds"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinSubResourceUpdate(ctx, test.service, test.subresource)
			got := test.service.Validate(ctx)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}

func getServiceSpec(image string) ServiceSpec {
	return ServiceSpec{
		ConfigurationSpec: ConfigurationSpec{
			Template: &RevisionTemplateSpec{
				Spec: RevisionSpec{
					RevisionSpec: v1.RevisionSpec{
						PodSpec: corev1.PodSpec{Containers: []corev1.Container{{
							Image: image,
						}},
						},
						TimeoutSeconds: ptr.Int64(config.DefaultMaxRevisionTimeoutSeconds),
					},
				},
			},
		},
		RouteSpec: RouteSpec{
			Traffic: []TrafficTarget{{
				TrafficTarget: v1.TrafficTarget{
					LatestRevision: ptr.Bool(true),
					Percent:        ptr.Int64(100)},
			}},
		},
	}
}

func TestServiceAnnotationUpdate(t *testing.T) {
	const (
		u1 = "oveja@knative.dev"
		u2 = "cabra@knative.dev"
		u3 = "vaca@knative.dev"
	)
	tests := []struct {
		name string
		prev *Service
		this *Service
		want *apis.FieldError
	}{{
		name: "update creator annotation",
		this: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u2,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getServiceSpec("helloworld:foo"),
		},
		prev: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getServiceSpec("helloworld:foo"),
		},
		want: &apis.FieldError{Message: "annotation value is immutable",
			Paths: []string{"metadata.annotations." + serving.CreatorAnnotation}},
	}, {
		name: "update lastModifier without spec changes",
		this: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u2,
				},
			},
			Spec: getServiceSpec("helloworld:foo"),
		},
		prev: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getServiceSpec("helloworld:foo"),
		},
		want: apis.ErrInvalidValue(u2, "metadata.annotations."+serving.UpdaterAnnotation),
	}, {
		name: "update lastModifier with spec changes",
		this: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u3,
				},
			},
			Spec: getServiceSpec("helloworld:bar"),
		},
		prev: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: getServiceSpec("helloworld:foo"),
		},
		want: nil,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinUpdate(ctx, test.prev)
			got := test.this.Validate(ctx)
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
