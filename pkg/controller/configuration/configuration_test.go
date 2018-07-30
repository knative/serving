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
	"testing"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/configuration/resources"
	"github.com/knative/serving/pkg/controller/testing/builder"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	. "github.com/knative/serving/pkg/controller/testing"
)

var (
	boolTrue  = true
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
		Objects: []runtime.Object{
			cfg("no-revisions-yet", "foo", 1234),
		},
		WantCreates: []metav1.Object{
			resources.MakeRevision(cfg("no-revisions-yet", "foo", 1234)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: builder.Config("no-revisions-yet", "foo", 1234).WithStatus(
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "no-revisions-yet-01234",
					ObservedGeneration:        1234,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				}),
		}},
		Key: "foo/no-revisions-yet",
	}, {
		Name: "webhook validation failure",
		// If we attempt to create a Revision with a bad ConcurrencyModel set, we fail.
		WantErr: true,
		Objects: []runtime.Object{
			builder.Config("validation-failure", "foo", 1234).WithConcurrencyModel("Bogus"),
		},
		WantCreates: []metav1.Object{
			setRevConcurrencyModel(resources.MakeRevision(cfg("validation-failure", "foo", 1234)), "Bogus"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: builder.Config("validation-failure", "foo", 1234).WithStatus(
				v1alpha1.ConfigurationStatus{
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:    v1alpha1.ConfigurationConditionReady,
						Status:  corev1.ConditionFalse,
						Reason:  "RevisionFailed",
						Message: `Revision creation failed with message: "invalid value \"Bogus\": spec.concurrencyModel".`,
					}},
				}).WithConcurrencyModel("Bogus"),
		}},
		Key: "foo/validation-failure",
	}, {
		Name: "create revision matching generation with build",
		Objects: []runtime.Object{
			builder.Config("need-rev-and-build", "foo", 99998).WithBuild(buildSpec),
		},
		WantCreates: []metav1.Object{
			resources.MakeBuild(builder.Config("need-rev-and-build", "foo", 99998).WithBuild(buildSpec).Build()),
			resources.MakeRevision(builder.Config("need-rev-and-build", "foo", 99998).WithBuild(buildSpec).Build()),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: builder.Config("need-rev-and-build", "foo", 99998).WithStatus(
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "need-rev-and-build-99998",
					ObservedGeneration:        99998,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			).WithBuild(buildSpec),
		}},
		Key: "foo/need-rev-and-build",
	}, {
		Name: "reconcile revision matching generation (ready: unknown)",
		Objects: []runtime.Object{
			cfg("matching-revision-not-done", "foo", 5432),
			resources.MakeRevision(cfg("matching-revision-not-done", "foo", 5432)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: builder.Config("matching-revision-not-done", "foo", 5432).WithStatus(
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-not-done-05432",
					ObservedGeneration:        5432,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			),
		}},
		Key: "foo/matching-revision-not-done",
	}, {
		Name: "reconcile revision matching generation (ready: true)",
		Objects: []runtime.Object{
			cfg("matching-revision-done", "foo", 5555),
			makeRevReady(t, resources.MakeRevision(cfg("matching-revision-done", "foo", 5555))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: builder.Config("matching-revision-done", "foo", 5555).WithStatus(
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-done-05555",
					LatestReadyRevisionName:   "matching-revision-done-05555",
					ObservedGeneration:        5555,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					}},
				},
			),
		}},
		Key: "foo/matching-revision-done",
	}, {
		Name: "reconcile revision matching generation (ready: true, idempotent)",
		Objects: []runtime.Object{
			builder.Config("matching-revision-done-idempotent", "foo", 5566).WithStatus(
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-done-idempotent-05566",
					LatestReadyRevisionName:   "matching-revision-done-idempotent-05566",
					ObservedGeneration:        5566,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					}},
				},
			),
			makeRevReady(t, resources.MakeRevision(cfg("matching-revision-done-idempotent", "foo", 5566))),
		},
		Key: "foo/matching-revision-done-idempotent",
	}, {
		Name: "reconcile revision matching generation (ready: false)",
		Objects: []runtime.Object{
			cfg("matching-revision-failed", "foo", 5555),
			makeRevFailed(resources.MakeRevision(cfg("matching-revision-failed", "foo", 5555))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: builder.Config("matching-revision-failed", "foo", 5555).WithStatus(
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-failed-05555",
					ObservedGeneration:        5555,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:    v1alpha1.ConfigurationConditionReady,
						Status:  corev1.ConditionFalse,
						Reason:  "RevisionFailed",
						Message: `Revision "matching-revision-failed-05555" failed with message: "It's the end of the world as we know it".`,
					}},
				},
			),
		}},
		Key: "foo/matching-revision-failed",
	}, {
		Name: "reconcile revision matching generation (ready: bad)",
		Objects: []runtime.Object{
			cfg("bad-condition", "foo", 5555),
			makeRevStatus(resources.MakeRevision(cfg("bad-condition", "foo", 5555)),
				v1alpha1.RevisionStatus{
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   v1alpha1.RevisionConditionReady,
						Status: "Bad",
					}},
				},
			),
		},
		WantErr: true,
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: builder.Config("bad-condition", "foo", 5555).WithStatus(
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "bad-condition-05555",
					ObservedGeneration:        5555,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			),
		}},
		Key: "foo/bad-condition",
	}, {
		Name: "failure creating build",
		// We induce a failure creating a build
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "builds"),
		},
		Objects: []runtime.Object{
			builder.Config("create-build-failure", "foo", 99998).WithBuild(buildSpec),
		},
		WantCreates: []metav1.Object{
			resources.MakeBuild(builder.Config("create-build-failure", "foo", 99998).WithBuild(buildSpec).Build()),
			// No Revision gets created.
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: builder.Config("create-build-failure", "foo", 99998).
				WithBuild(buildSpec).
				WithStatus(
					v1alpha1.ConfigurationStatus{
						Conditions: []v1alpha1.ConfigurationCondition{{
							Type:    v1alpha1.ConfigurationConditionReady,
							Status:  corev1.ConditionFalse,
							Reason:  "RevisionFailed",
							Message: `Revision creation failed with message: "inducing failure for create builds".`,
						}},
					},
				),
		}},
		Key: "foo/create-build-failure",
	}, {
		Name: "failure creating revision",
		// We induce a failure creating a revision
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "revisions"),
		},
		Objects: []runtime.Object{
			cfg("create-revision-failure", "foo", 99998),
		},
		WantCreates: []metav1.Object{
			resources.MakeRevision(cfg("create-revision-failure", "foo", 99998)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: builder.Config("create-revision-failure", "foo", 99998).WithStatus(
				v1alpha1.ConfigurationStatus{
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:    v1alpha1.ConfigurationConditionReady,
						Status:  corev1.ConditionFalse,
						Reason:  "RevisionFailed",
						Message: `Revision creation failed with message: "inducing failure for create revisions".`,
					}},
				},
			),
		}},
		Key: "foo/create-revision-failure",
	}, {
		Name: "failure updating configuration status",
		// Induce a failure updating the status of the configuration.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "configurations"),
		},
		Objects: []runtime.Object{
			cfg("update-config-failure", "foo", 1234),
		},
		WantCreates: []metav1.Object{
			resources.MakeRevision(cfg("update-config-failure", "foo", 1234)),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: builder.Config("update-config-failure", "foo", 1234).WithStatus(
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "update-config-failure-01234",
					ObservedGeneration:        1234,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			),
		}},
		Key: "foo/update-config-failure",
	}, {
		Name: "failed revision recovers",
		Objects: []runtime.Object{
			builder.Config("revision-recovers", "foo", 1337).WithStatus(
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "revision-recovers-01337",
					LatestReadyRevisionName:   "revision-recovers-01337",
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:    v1alpha1.ConfigurationConditionReady,
						Status:  corev1.ConditionFalse,
						Reason:  "RevisionFailed",
						Message: `Revision "revision-recovers-01337" failed with message: "Weebles wobble, but they don't fall down".`,
					}},
				},
			),
			makeRevReady(t, resources.MakeRevision(cfg("revision-recovers", "foo", 1337))),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: builder.Config("revision-recovers", "foo", 1337).WithStatus(
				v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "revision-recovers-01337",
					LatestReadyRevisionName:   "revision-recovers-01337",
					ObservedGeneration:        1337,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					}},
				},
			),
		}},
		Key: "foo/revision-recovers",
	}}

	table.Test(t, func(listers *Listers, opt controller.Options) controller.Interface {
		return &Controller{
			Base:                controller.NewBase(opt, controllerAgentName, "Configurations"),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
		}
	})
}

func setRevConcurrencyModel(rev *v1alpha1.Revision, ss v1alpha1.RevisionRequestConcurrencyModelType) *v1alpha1.Revision {
	rev.Spec.ConcurrencyModel = ss
	return rev
}

func makeRevReady(t *testing.T, rev *v1alpha1.Revision) *v1alpha1.Revision {
	rev.Status.InitializeConditions()
	rev.Status.MarkContainerHealthy()
	rev.Status.MarkResourcesAvailable()
	if !rev.Status.IsReady() {
		t.Fatalf("Wanted ready revision: %v", rev)
	}
	return rev
}

func makeRevFailed(rev *v1alpha1.Revision) *v1alpha1.Revision {
	rev.Status.InitializeConditions()
	rev.Status.MarkContainerMissing("It's the end of the world as we know it")
	return rev
}

func makeRevStatus(rev *v1alpha1.Revision, status v1alpha1.RevisionStatus) *v1alpha1.Revision {
	rev.Status = status
	return rev
}

func cfg(name, namespace string, generation int64) *v1alpha1.Configuration {
	return builder.Config(name, namespace, generation).Build()
}
