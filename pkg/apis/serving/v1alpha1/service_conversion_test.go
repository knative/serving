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

	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
)

func TestServiceConversionBadType(t *testing.T) {
	good, bad := &Service{}, &Revision{}

	if err := good.ConvertUp(context.Background(), bad); err == nil {
		t.Errorf("ConvertUp() = %#v, wanted error", bad)
	}

	if err := good.ConvertDown(context.Background(), bad); err == nil {
		t.Errorf("ConvertDown() = %#v, wanted error", good)
	}
}

func TestServiceConversion(t *testing.T) {
	versions := []apis.Convertible{&v1.Service{}, &v1beta1.Service{}}

	tests := []struct {
		name string
		in   *Service
	}{{
		name: "simple conversion",
		in: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
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
								ContainerConcurrency: ptr.Int64(53),
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							Tag:            "latest",
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					}},
				},
			},
			Status: ServiceStatus{
				Status: duckv1.Status{
					ObservedGeneration: 1,
					Conditions: duckv1.Conditions{{
						Type:   "Ready",
						Status: "True",
					}},
				},
				ConfigurationStatusFields: ConfigurationStatusFields{
					LatestCreatedRevisionName: "foo-00002",
					LatestReadyRevisionName:   "foo-00002",
				},
				RouteStatusFields: RouteStatusFields{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							Tag:            "latest",
							Percent:        ptr.Int64(100),
							RevisionName:   "foo-00001",
							LatestRevision: ptr.Bool(true),
						},
					}},
				},
			},
		},
	}}

	for _, test := range tests {
		for _, version := range versions {
			t.Run(test.name, func(t *testing.T) {
				ver := version
				if err := test.in.ConvertUp(context.Background(), ver); err != nil {
					t.Errorf("ConvertUp() = %v", err)
				}
				t.Logf("ConvertUp() = %#v", ver)
				got := &Service{}
				if err := got.ConvertDown(context.Background(), ver); err != nil {
					t.Errorf("ConvertDown() = %v", err)
				}
				t.Logf("ConvertDown() = %#v", got)
				if diff := cmp.Diff(test.in, got); diff != "" {
					t.Errorf("roundtrip (-want, +got) = %v", diff)
				}
			})
		}
	}
}

func TestServiceConversionFromDeprecated(t *testing.T) {
	status := ServiceStatus{
		Status: duckv1.Status{
			ObservedGeneration: 1,
			Conditions: duckv1.Conditions{{
				Type:   "Ready",
				Status: "True",
			}},
		},
		ConfigurationStatusFields: ConfigurationStatusFields{
			LatestCreatedRevisionName: "foo-00002",
			LatestReadyRevisionName:   "foo-00002",
		},
		RouteStatusFields: RouteStatusFields{
			Traffic: []TrafficTarget{{
				TrafficTarget: v1.TrafficTarget{
					Percent:      ptr.Int64(100),
					RevisionName: "foo-00001",
				},
			}},
		},
	}

	versions := []apis.Convertible{&v1.Service{}, &v1beta1.Service{}}
	tests := []struct {
		name     string
		in       *Service
		want     *Service
		badField string
	}{{
		name: "run latest",
		in: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: ServiceSpec{
				DeprecatedRunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1.RevisionSpec{
									PodSpec: corev1.PodSpec{
										ServiceAccountName: "robocop",
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
									ContainerConcurrency: ptr.Int64(53),
								},
								DeprecatedContainer: &corev1.Container{
									Image: "busybox",
									VolumeMounts: []corev1.VolumeMount{{
										MountPath: "/mount/path",
										Name:      "the-name",
										ReadOnly:  true,
									}},
								},
							},
						},
					},
				},
			},
			Status: status,
		},
		want: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
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
								ContainerConcurrency: ptr.Int64(53),
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							Percent:        ptr.Int64(100),
							LatestRevision: ptr.Bool(true),
						},
					}},
				},
			},
			Status: status,
		},
	}, {
		name: "release single",
		in: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions: []string{"foo-00001"},
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1.RevisionSpec{
									PodSpec: corev1.PodSpec{
										ServiceAccountName: "robocop",
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
									ContainerConcurrency: ptr.Int64(53),
								},
								DeprecatedContainer: &corev1.Container{
									Image: "busybox",
									VolumeMounts: []corev1.VolumeMount{{
										MountPath: "/mount/path",
										Name:      "the-name",
										ReadOnly:  true,
									}},
								},
							},
						},
					},
				},
			},
			Status: status,
		},
		want: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
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
								ContainerConcurrency: ptr.Int64(53),
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							Tag:          "current",
							RevisionName: "foo-00001",
							Percent:      ptr.Int64(100),
						},
					}, {
						TrafficTarget: v1.TrafficTarget{
							Tag:            "latest",
							LatestRevision: ptr.Bool(true),
							Percent:        nil,
						},
					}},
				},
			},
			Status: status,
		},
	}, {
		name: "release double",
		in: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions:      []string{"foo-00001", "foo-00002"},
					RolloutPercent: 22,
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1.RevisionSpec{
									PodSpec: corev1.PodSpec{
										ServiceAccountName: "robocop",
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
									ContainerConcurrency: ptr.Int64(53),
								},
								DeprecatedContainer: &corev1.Container{
									Image: "busybox",
									VolumeMounts: []corev1.VolumeMount{{
										MountPath: "/mount/path",
										Name:      "the-name",
										ReadOnly:  true,
									}},
								},
							},
						},
					},
				},
			},
			Status: status,
		},
		want: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
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
								ContainerConcurrency: ptr.Int64(53),
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							Tag:          "current",
							RevisionName: "foo-00001",
							Percent:      ptr.Int64(78),
						},
					}, {
						TrafficTarget: v1.TrafficTarget{
							Tag:          "candidate",
							RevisionName: "foo-00002",
							Percent:      ptr.Int64(22),
						},
					}, {
						TrafficTarget: v1.TrafficTarget{
							Tag:            "latest",
							LatestRevision: ptr.Bool(true),
							Percent:        nil,
						},
					}},
				},
			},
			Status: status,
		},
	}, {
		name: "release double w/ @latest",
		in: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: ServiceSpec{
				DeprecatedRelease: &ReleaseType{
					Revisions:      []string{"foo-00001", "@latest"},
					RolloutPercent: 37,
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1.RevisionSpec{
									PodSpec: corev1.PodSpec{
										ServiceAccountName: "robocop",
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
									ContainerConcurrency: ptr.Int64(53),
								},
								DeprecatedContainer: &corev1.Container{
									Image: "busybox",
									VolumeMounts: []corev1.VolumeMount{{
										MountPath: "/mount/path",
										Name:      "the-name",
										ReadOnly:  true,
									}},
								},
							},
						},
					},
				},
			},
			Status: status,
		},
		want: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
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
								ContainerConcurrency: ptr.Int64(53),
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							Tag:          "current",
							RevisionName: "foo-00001",
							Percent:      ptr.Int64(63),
						},
					}, {
						TrafficTarget: v1.TrafficTarget{
							Tag:            "candidate",
							LatestRevision: ptr.Bool(true),
							Percent:        ptr.Int64(37),
						},
					}, {
						TrafficTarget: v1.TrafficTarget{
							Tag:            "latest",
							LatestRevision: ptr.Bool(true),
							Percent:        nil,
						},
					}},
				},
			},
			Status: status,
		},
	}, {
		name: "pinned",
		in: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: ServiceSpec{
				DeprecatedPinned: &PinnedType{
					RevisionName: "foo-00001",
					Configuration: ConfigurationSpec{
						DeprecatedRevisionTemplate: &RevisionTemplateSpec{
							Spec: RevisionSpec{
								RevisionSpec: v1.RevisionSpec{
									PodSpec: corev1.PodSpec{
										ServiceAccountName: "robocop",
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
									ContainerConcurrency: ptr.Int64(53),
								},
								DeprecatedContainer: &corev1.Container{
									Image: "busybox",
									VolumeMounts: []corev1.VolumeMount{{
										MountPath: "/mount/path",
										Name:      "the-name",
										ReadOnly:  true,
									}},
								},
							},
						},
					},
				},
			},
			Status: status,
		},
		want: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: ServiceSpec{
				ConfigurationSpec: ConfigurationSpec{
					Template: &RevisionTemplateSpec{
						Spec: RevisionSpec{
							RevisionSpec: v1.RevisionSpec{
								PodSpec: corev1.PodSpec{
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
								ContainerConcurrency: ptr.Int64(53),
							},
						},
					},
				},
				RouteSpec: RouteSpec{
					Traffic: []TrafficTarget{{
						TrafficTarget: v1.TrafficTarget{
							RevisionName: "foo-00001",
							Percent:      ptr.Int64(100),
						},
					}},
				},
			},
			Status: status,
		},
	}, {
		name: "manual",
		in: &Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "asdf",
				Namespace:  "blah",
				Generation: 1,
			},
			Spec: ServiceSpec{
				DeprecatedManual: &ManualType{},
			},
			Status: status,
		},
		badField: "manual",
	}}

	for _, test := range tests {
		for _, version := range versions {
			t.Run(test.name, func(t *testing.T) {
				ver := version
				if err := test.in.ConvertUp(context.Background(), ver); err != nil {
					if test.badField != "" {
						cce, ok := err.(*CannotConvertError)
						if ok && cce.Field == test.badField {
							return
						}
					}
					t.Errorf("ConvertUp() = %v", err)
				}
				t.Logf("ConvertUp() = %#v", ver)
				got := &Service{}
				if err := got.ConvertDown(context.Background(), ver); err != nil {
					t.Errorf("ConvertDown() = %v", err)
				}
				t.Logf("ConvertDown() = %#v", got)
				if diff := cmp.Diff(test.want, got); diff != "" {
					t.Errorf("roundtrip (-want, +got) = %v", diff)
				}
			})
		}
	}
}
