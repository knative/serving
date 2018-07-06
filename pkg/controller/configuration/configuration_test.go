/*
Copyright 2018 The Knative Authors.

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

package configuration

import (
	"fmt"
	"testing"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgotesting "k8s.io/client-go/testing"

	. "github.com/knative/serving/pkg/controller/testing"
)

var (
	boolTrue     = true
	revisionSpec = v1alpha1.RevisionSpec{
		Container: corev1.Container{
			Image: "busybox",
		},
	}
	buildSpec = buildv1alpha1.BuildSpec{
		Steps: []corev1.Container{{
			Image: "build-step1",
		}, {
			Image: "build-step2",
		}},
	}
)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name: "bad workqueue key",
		Key:  "too/many/parts",
	}, {
		Name: "key not found",
		Key:  "foo/not-found",
	}, {
		Name: "create revision matching generation",
		Listers: Listers{
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "no-revisions-yet"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 1234,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: revisionSpec,
						},
					},
				}},
			},
		},
		WantCreates: []metav1.Object{
			&v1alpha1.Revision{
				ObjectMeta: com("foo", "no-revisions-yet", 1234),
				Spec:       revisionSpec,
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "no-revisions-yet"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 1234,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "no-revisions-yet-01234",
					ObservedGeneration:        1234,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
		Key: "foo/no-revisions-yet",
	}, {
		Name: "create revision matching generation with build",
		Listers: Listers{
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "need-rev-and-build"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 99998,
						Build:      &buildSpec,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: v1alpha1.RevisionSpec{
								Container: corev1.Container{
									Image: "busybox",
								},
							},
						},
					},
				}},
			},
		},
		WantCreates: []metav1.Object{
			&buildv1alpha1.Build{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       "foo",
					Name:            "need-rev-and-build-99998",
					OwnerReferences: or("need-rev-and-build"),
				},
				Spec: buildSpec,
			},
			&v1alpha1.Revision{
				ObjectMeta: com("foo", "need-rev-and-build", 99998),
				Spec: v1alpha1.RevisionSpec{
					BuildName: "need-rev-and-build-99998",
					Container: corev1.Container{
						Image: "busybox",
					},
				},
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "need-rev-and-build"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 99998,
					Build:      &buildSpec,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "need-rev-and-build-99998",
					ObservedGeneration:        99998,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
		Key: "foo/need-rev-and-build",
	}, {
		Name: "reconcile revision matching generation (ready: unknown)",
		Listers: Listers{
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "matching-revision-not-done"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 5432,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: revisionSpec,
						},
					},
				}},
			},
			Revision: &RevisionLister{
				Items: []*v1alpha1.Revision{{
					ObjectMeta: com("foo", "matching-revision-not-done", 5432),
					Spec:       revisionSpec,
				}},
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "matching-revision-not-done"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 5432,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-not-done-05432",
					ObservedGeneration:        5432,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
		Key: "foo/matching-revision-not-done",
	}, {
		Name: "reconcile revision matching generation (ready: true)",
		Listers: Listers{
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "matching-revision-done"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 5555,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: revisionSpec,
						},
					},
				}},
			},
			Revision: &RevisionLister{
				Items: []*v1alpha1.Revision{{
					ObjectMeta: com("foo", "matching-revision-done", 5555),
					Spec:       revisionSpec,
					Status: v1alpha1.RevisionStatus{
						Conditions: []v1alpha1.RevisionCondition{{
							Type:   v1alpha1.RevisionConditionReady,
							Status: corev1.ConditionTrue,
						}},
					},
				}},
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "matching-revision-done"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 5555,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-done-05555",
					LatestReadyRevisionName:   "matching-revision-done-05555",
					ObservedGeneration:        5555,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					}},
				},
			},
		}},
		Key: "foo/matching-revision-done",
	}, {
		Name: "reconcile revision matching generation (ready: true, idempotent)",
		Listers: Listers{
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "matching-revision-done-idempotent"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 5566,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: revisionSpec,
						},
					},
					Status: v1alpha1.ConfigurationStatus{
						LatestCreatedRevisionName: "matching-revision-done-idempotent-05566",
						LatestReadyRevisionName:   "matching-revision-done-idempotent-05566",
						ObservedGeneration:        5566,
						Conditions: []v1alpha1.ConfigurationCondition{{
							Type:   v1alpha1.ConfigurationConditionReady,
							Status: corev1.ConditionTrue,
						}},
					},
				}},
			},
			Revision: &RevisionLister{
				Items: []*v1alpha1.Revision{{
					ObjectMeta: com("foo", "matching-revision-done-idempotent", 5566),
					Spec:       revisionSpec,
					Status: v1alpha1.RevisionStatus{
						Conditions: []v1alpha1.RevisionCondition{{
							Type:   v1alpha1.RevisionConditionReady,
							Status: corev1.ConditionTrue,
						}},
					},
				}},
			},
		},
		Key: "foo/matching-revision-done-idempotent",
	}, {
		Name: "reconcile revision matching generation (ready: false)",
		Listers: Listers{
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "matching-revision-failed"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 5555,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: revisionSpec,
						},
					},
				}},
			},
			Revision: &RevisionLister{
				Items: []*v1alpha1.Revision{{
					ObjectMeta: com("foo", "matching-revision-failed", 5555),
					Spec:       revisionSpec,
					Status: v1alpha1.RevisionStatus{
						Conditions: []v1alpha1.RevisionCondition{{
							Type:    v1alpha1.RevisionConditionReady,
							Status:  corev1.ConditionFalse,
							Reason:  "Armageddon",
							Message: "It's the end of the world as we know it.",
						}},
					},
				}},
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "matching-revision-failed"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 5555,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-failed-05555",
					ObservedGeneration:        5555,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:    v1alpha1.ConfigurationConditionReady,
						Status:  corev1.ConditionFalse,
						Reason:  "RevisionFailed",
						Message: `revision "matching-revision-failed-05555" failed with message: It's the end of the world as we know it.`,
					}},
				},
			},
		}},
		Key: "foo/matching-revision-failed",
	}, {
		Name: "reconcile revision matching generation (ready: bad)",
		Listers: Listers{
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "bad-condition"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 5555,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: revisionSpec,
						},
					},
				}},
			},
			Revision: &RevisionLister{
				Items: []*v1alpha1.Revision{{
					ObjectMeta: com("foo", "bad-condition", 5555),
					Spec:       revisionSpec,
					Status: v1alpha1.RevisionStatus{
						Conditions: []v1alpha1.RevisionCondition{{
							Type:   v1alpha1.RevisionConditionReady,
							Status: "Bad",
						}},
					},
				}},
			},
		},
		WantErr: true,
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "bad-condition"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 5555,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "bad-condition-05555",
					ObservedGeneration:        5555,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
		Key: "foo/bad-condition",
	}, {
		Name: "failure creating build",
		// We induce a failure creating a build
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "builds"),
		},
		Listers: Listers{
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "create-build-failure"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 99998,
						Build:      &buildSpec,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: v1alpha1.RevisionSpec{
								Container: corev1.Container{
									Image: "busybox",
								},
							},
						},
					},
				}},
			},
		},
		WantCreates: []metav1.Object{
			&buildv1alpha1.Build{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       "foo",
					Name:            "create-build-failure-99998",
					OwnerReferences: or("create-build-failure"),
				},
				Spec: buildSpec,
			},
			// No Revision gets created.
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "create-build-failure"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 99998,
					Build:      &buildSpec,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
		Key: "foo/create-build-failure",
	}, {
		Name: "failure creating revision",
		// We induce a failure creating a revision
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "revisions"),
		},
		Listers: Listers{
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "create-revision-failure"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 99998,
						Build:      &buildSpec,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: v1alpha1.RevisionSpec{
								Container: corev1.Container{
									Image: "busybox",
								},
							},
						},
					},
				}},
			},
		},
		WantCreates: []metav1.Object{
			&buildv1alpha1.Build{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       "foo",
					Name:            "create-revision-failure-99998",
					OwnerReferences: or("create-revision-failure"),
				},
				Spec: buildSpec,
			},
			&v1alpha1.Revision{
				ObjectMeta: com("foo", "create-revision-failure", 99998),
				Spec: v1alpha1.RevisionSpec{
					BuildName: "create-revision-failure-99998",
					Container: corev1.Container{
						Image: "busybox",
					},
				},
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "create-revision-failure"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 99998,
					Build:      &buildSpec,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
		Key: "foo/create-revision-failure",
	}, {
		Name: "failure updating configuration status",
		// Induce a failure updating the status of the configuration.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "configurations"),
		},
		Listers: Listers{
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "update-config-failure"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 1234,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: revisionSpec,
						},
					},
				}},
			},
		},
		WantCreates: []metav1.Object{
			&v1alpha1.Revision{
				ObjectMeta: com("foo", "update-config-failure", 1234),
				Spec:       revisionSpec,
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "update-config-failure"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 1234,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "update-config-failure-01234",
					ObservedGeneration:        1234,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
		Key: "foo/update-config-failure",
	}}

	table.Test(t, func(listers *Listers, opt controller.Options) controller.Interface {
		return &Controller{
			Base:                controller.NewBase(opt, controllerAgentName, "Configurations"),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
		}
	})
}

// or builds OwnerReferences for a child of a Configuration
func or(name string) []metav1.OwnerReference {
	return []metav1.OwnerReference{{
		APIVersion:         v1alpha1.SchemeGroupVersion.String(),
		Kind:               "Configuration",
		Name:               name,
		Controller:         &boolTrue,
		BlockOwnerDeletion: &boolTrue,
	}}
}

// om builds ObjectMeta for a Configuration
func om(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
}

// com builds the ObjectMeta for a Child of a Configuration
func com(namespace, name string, generation int) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      fmt.Sprintf("%s-%05d", name, generation),
		Namespace: namespace,
		Annotations: map[string]string{
			serving.ConfigurationGenerationAnnotationKey: fmt.Sprintf("%v", generation),
		},
		Labels: map[string]string{
			serving.ConfigurationLabelKey: name,
		},
		OwnerReferences: or(name),
	}
}
