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
			builder.Config("no-revisions-yet", "foo", 1234),
		},
		WantCreates: []metav1.Object{
			builder.Config("no-revisions-yet", "foo", 1234).
				ToRevision().
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			builder.Config("no-revisions-yet", "foo", 1234).
				WithLatestCreatedRevisionName("no-revisions-yet-01234").
				WithObservedGeneration(1234).
				WithCondition(v1alpha1.ConfigurationCondition{
					Type:   v1alpha1.ConfigurationConditionReady,
					Status: corev1.ConditionUnknown,
				}).AsUpdateAction(),
		},
		Key: "foo/no-revisions-yet",
	}, {
		Name: "webhook validation failure",
		// If we attempt to create a Revision with a bad ConcurrencyModel set, we fail.
		WantErr: true,
		Objects: []runtime.Object{
			builder.Config("validation-failure", "foo", 1234).WithConcurrencyModel("Bogus"),
		},
		WantCreates: []metav1.Object{
			builder.Config("validation-failure", "foo", 1234).
				ToRevision().
				WithConcurrencyModel("Bogus").
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			builder.Config("validation-failure", "foo", 1234).
				WithConcurrencyModel("Bogus").
				WithCondition(v1alpha1.ConfigurationCondition{
					Type:    v1alpha1.ConfigurationConditionReady,
					Status:  corev1.ConditionFalse,
					Reason:  "RevisionFailed",
					Message: `Revision creation failed with message: "invalid value \"Bogus\": spec.concurrencyModel".`,
				}).AsUpdateAction(),
		},
		Key: "foo/validation-failure",
	}, {
		Name: "create revision matching generation with build",
		Objects: []runtime.Object{
			builder.Config("need-rev-and-build", "foo", 99998).WithBuild(buildSpec),
		},
		WantCreates: []metav1.Object{
			builder.Config("need-rev-and-build", "foo", 99998).WithBuild(buildSpec).
				ToBuild(),
			builder.Config("need-rev-and-build", "foo", 99998).WithBuild(buildSpec).
				ToRevision().
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			builder.Config("need-rev-and-build", "foo", 99998).
				WithBuild(buildSpec).
				WithLatestCreatedRevisionName("need-rev-and-build-99998").
				WithObservedGeneration(99998).
				WithCondition(v1alpha1.ConfigurationCondition{
					Type:   v1alpha1.ConfigurationConditionReady,
					Status: corev1.ConditionUnknown,
				}).AsUpdateAction(),
		},
		Key: "foo/need-rev-and-build",
	}, {
		Name: "reconcile revision matching generation (ready: unknown)",
		Objects: []runtime.Object{
			builder.Config("matching-revision-not-done", "foo", 5432),
			builder.Config("matching-revision-not-done", "foo", 5432).
				ToRevision(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			builder.Config("matching-revision-not-done", "foo", 5432).
				WithLatestCreatedRevisionName("matching-revision-not-done-05432").
				WithObservedGeneration(5432).
				WithCondition(v1alpha1.ConfigurationCondition{
					Type:   v1alpha1.ConfigurationConditionReady,
					Status: corev1.ConditionUnknown,
				}).AsUpdateAction(),
		},
		Key: "foo/matching-revision-not-done",
	}, {
		Name: "reconcile revision matching generation (ready: true)",
		Objects: []runtime.Object{
			builder.Config("matching-revision-done", "foo", 5555),
			builder.Config("matching-revision-done", "foo", 5555).
				ToRevision().
				WithReadyStatus(t),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			builder.Config("matching-revision-done", "foo", 5555).
				WithLatestCreatedRevisionName("matching-revision-done-05555").
				WithLatestReadyRevisionName("matching-revision-done-05555").
				WithObservedGeneration(5555).
				WithCondition(v1alpha1.ConfigurationCondition{
					Type:   v1alpha1.ConfigurationConditionReady,
					Status: corev1.ConditionTrue,
				}).AsUpdateAction(),
		},
		Key: "foo/matching-revision-done",
	}, {
		Name: "reconcile revision matching generation (ready: true, idempotent)",
		Objects: []runtime.Object{
			builder.Config("matching-revision-done-idempotent", "foo", 5566).
				WithLatestCreatedRevisionName("matching-revision-done-idempotent-05566").
				WithLatestReadyRevisionName("matching-revision-done-idempotent-05566").
				WithObservedGeneration(5566).
				WithCondition(v1alpha1.ConfigurationCondition{
					Type:   v1alpha1.ConfigurationConditionReady,
					Status: corev1.ConditionTrue,
				}),
			builder.Config("matching-revision-done-idempotent", "foo", 5566).
				ToRevision().
				WithReadyStatus(t),
		},
		Key: "foo/matching-revision-done-idempotent",
	}, {
		Name: "reconcile revision matching generation (ready: false)",
		Objects: []runtime.Object{
			builder.Config("matching-revision-failed", "foo", 5555),
			builder.Config("matching-revision-failed", "foo", 5555).
				ToRevision().
				WithFailedStatus(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			builder.Config("matching-revision-failed", "foo", 5555).
				WithLatestCreatedRevisionName("matching-revision-failed-05555").
				WithObservedGeneration(5555).
				WithCondition(v1alpha1.ConfigurationCondition{
					Type:    v1alpha1.ConfigurationConditionReady,
					Status:  corev1.ConditionFalse,
					Reason:  "RevisionFailed",
					Message: `Revision "matching-revision-failed-05555" failed with message: "It's the end of the world as we know it".`,
				}).AsUpdateAction(),
		},
		Key: "foo/matching-revision-failed",
	}, {
		Name: "reconcile revision matching generation (ready: bad)",
		Objects: []runtime.Object{
			builder.Config("bad-condition", "foo", 5555),
			builder.Config("bad-condition", "foo", 5555).
				ToRevision().
				WithCondition(v1alpha1.RevisionCondition{
					Type:   v1alpha1.RevisionConditionReady,
					Status: "Bad",
				}),
		},
		WantErr: true,
		WantUpdates: []clientgotesting.UpdateActionImpl{
			builder.Config("bad-condition", "foo", 5555).
				WithLatestCreatedRevisionName("bad-condition-05555").
				WithObservedGeneration(5555).
				WithCondition(v1alpha1.ConfigurationCondition{
					Type:   v1alpha1.ConfigurationConditionReady,
					Status: corev1.ConditionUnknown,
				}).AsUpdateAction(),
		},
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
			builder.Config("create-build-failure", "foo", 99998).
				WithBuild(buildSpec).
				ToBuild(),
			// No Revision gets created.
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			builder.Config("create-build-failure", "foo", 99998).
				WithBuild(buildSpec).
				WithCondition(v1alpha1.ConfigurationCondition{
					Type:    v1alpha1.ConfigurationConditionReady,
					Status:  corev1.ConditionFalse,
					Reason:  "RevisionFailed",
					Message: `Revision creation failed with message: "inducing failure for create builds".`,
				}).AsUpdateAction(),
		},
		Key: "foo/create-build-failure",
	}, {
		Name: "failure creating revision",
		// We induce a failure creating a revision
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "revisions"),
		},
		Objects: []runtime.Object{
			builder.Config("create-revision-failure", "foo", 99998),
		},
		WantCreates: []metav1.Object{
			builder.Config("create-revision-failure", "foo", 99998).
				ToRevision().
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			builder.Config("create-revision-failure", "foo", 99998).
				WithCondition(v1alpha1.ConfigurationCondition{
					Type:    v1alpha1.ConfigurationConditionReady,
					Status:  corev1.ConditionFalse,
					Reason:  "RevisionFailed",
					Message: `Revision creation failed with message: "inducing failure for create revisions".`,
				}).AsUpdateAction(),
		},
		Key: "foo/create-revision-failure",
	}, {
		Name: "failure updating configuration status",
		// Induce a failure updating the status of the configuration.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "configurations"),
		},
		Objects: []runtime.Object{
			builder.Config("update-config-failure", "foo", 1234),
		},
		WantCreates: []metav1.Object{
			builder.Config("update-config-failure", "foo", 1234).
				ToRevision().
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			builder.Config("update-config-failure", "foo", 1234).
				WithLatestCreatedRevisionName("update-config-failure-01234").
				WithObservedGeneration(1234).
				WithCondition(v1alpha1.ConfigurationCondition{
					Type:   v1alpha1.ConfigurationConditionReady,
					Status: corev1.ConditionUnknown,
				}).AsUpdateAction(),
		},
		Key: "foo/update-config-failure",
	}, {
		Name: "failed revision recovers",
		Objects: []runtime.Object{
			builder.Config("revision-recovers", "foo", 1337).
				WithLatestCreatedRevisionName("revision-recovers-01337").
				WithLatestReadyRevisionName("revision-recovers-01337").
				WithCondition(
					v1alpha1.ConfigurationCondition{
						Type:    v1alpha1.ConfigurationConditionReady,
						Status:  corev1.ConditionFalse,
						Reason:  "RevisionFailed",
						Message: `Revision "revision-recovers-01337" failed with message: "Weebles wobble, but they don't fall down".`,
					}),
			builder.Config("revision-recovers", "foo", 1337).
				ToRevision().
				WithReadyStatus(t),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			builder.Config("revision-recovers", "foo", 1337).
				WithLatestCreatedRevisionName("revision-recovers-01337").
				WithLatestReadyRevisionName("revision-recovers-01337").
				WithObservedGeneration(1337).
				WithCondition(v1alpha1.ConfigurationCondition{
					Type:   v1alpha1.ConfigurationConditionReady,
					Status: corev1.ConditionTrue,
				}).AsUpdateAction(),
		},
		Key: "foo/revision-recovers",
	}}

	table.Test(t, func(listers *Listers, opt controller.ReconcileOptions) controller.Reconciler {
		return &Reconciler{
			Base:                controller.NewBase(opt, controllerAgentName),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
		}
	})
}
