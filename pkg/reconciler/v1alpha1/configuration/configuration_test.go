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
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/configuration/resources"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	. "github.com/knative/serving/pkg/apis/serving/v1alpha1/testing"
	. "github.com/knative/serving/pkg/reconciler/testing"
)

var (
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
		Key:  "default/not-found",
	}, {
		Name: "create revision matching generation",
		Objects: []runtime.Object{
			Config("no-revisions-yet").
				WithGeneration(1234).
				Build(),
		},
		WantCreates: []metav1.Object{
			Config("no-revisions-yet").
				WithGeneration(1234).
				ToChildRevision(resources.MakeRevision).
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			Config("no-revisions-yet").
				WithGeneration(1234).
				WithLatestCreatedRevisionName("no-revisions-yet-01234").
				WithObservedGeneration(1234).
				WithReadyConditionUnknown().
				ToUpdateAction(),
		},
		Key: "default/no-revisions-yet",
	}, {
		Name: "webhook validation failure",
		// If we attempt to create a Revision with a bad ConcurrencyModel set, we fail.
		WantErr: true,
		Objects: []runtime.Object{
			Config("validation-failure").
				WithGeneration(1234).
				WithConcurrencyModel("Bogus").
				Build(),
		},
		WantCreates: []metav1.Object{
			Config("validation-failure").
				WithGeneration(1234).
				WithConcurrencyModel("Bogus").
				ToChildRevision(resources.MakeRevision).
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			Config("validation-failure").
				WithGeneration(1234).
				WithConcurrencyModel("Bogus").
				WithReadyConditionFalse(
					"RevisionFailed",
					`Revision creation failed with message: "invalid value \"Bogus\": spec.concurrencyModel".`,
				).
				ToUpdateAction(),
		},
		Key: "default/validation-failure",
	}, {
		Name: "create revision matching generation with build",
		Objects: []runtime.Object{
			Config("need-rev-and-build").
				WithGeneration(99998).
				WithBuild(buildSpec).
				Build(),
		},
		WantCreates: []metav1.Object{
			Config("need-rev-and-build").
				WithGeneration(99998).
				WithBuild(buildSpec).
				ToChildBuild(resources.MakeBuild),
			Config("need-rev-and-build").
				WithGeneration(99998).
				WithBuild(buildSpec).
				ToChildRevision(resources.MakeRevision).
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			Config("need-rev-and-build").
				WithGeneration(99998).
				WithBuild(buildSpec).
				WithLatestCreatedRevisionName("need-rev-and-build-99998").
				WithObservedGeneration(99998).
				WithReadyConditionUnknown().
				ToUpdateAction(),
		},
		Key: "default/need-rev-and-build",
	}, {
		Name: "reconcile revision matching generation (ready: unknown)",
		Objects: []runtime.Object{
			Config("matching-revision-not-done").
				WithGeneration(5432).
				Build(),
			Config("matching-revision-not-done").
				WithGeneration(5432).
				ToChildRevision(resources.MakeRevision).
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			Config("matching-revision-not-done").
				WithGeneration(5432).
				WithLatestCreatedRevisionName("matching-revision-not-done-05432").
				WithObservedGeneration(5432).
				WithReadyConditionUnknown().
				ToUpdateAction(),
		},
		Key: "default/matching-revision-not-done",
	}, {
		Name: "reconcile revision matching generation (ready: true)",
		Objects: []runtime.Object{
			Config("matching-revision-done").
				WithGeneration(5555).
				Build(),
			Config("matching-revision-done").
				WithGeneration(5555).
				ToChildRevision(resources.MakeRevision).
				WithReadyConditionTrue().
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			Config("matching-revision-done").
				WithGeneration(5555).
				WithLatestCreatedRevisionName("matching-revision-done-05555").
				WithLatestReadyRevisionName("matching-revision-done-05555").
				WithObservedGeneration(5555).
				WithReadyConditionTrue().
				ToUpdateAction(),
		},
		Key: "default/matching-revision-done",
	}, {
		Name: "reconcile revision matching generation (ready: true, idempotent)",
		Objects: []runtime.Object{
			Config("matching-revision-done-idempotent").
				WithGeneration(5566).
				WithLatestCreatedRevisionName("matching-revision-done-idempotent-05566").
				WithLatestReadyRevisionName("matching-revision-done-idempotent-05566").
				WithObservedGeneration(5566).
				WithReadyConditionTrue().
				Build(),
			Config("matching-revision-done-idempotent").
				WithGeneration(5566).
				ToChildRevision(resources.MakeRevision).
				WithReadyConditionTrue().
				Build(),
		},
		Key: "default/matching-revision-done-idempotent",
	}, {
		Name: "reconcile revision matching generation (ready: false)",
		Objects: []runtime.Object{
			Config("matching-revision-failed").
				WithGeneration(5555).
				Build(),
			Config("matching-revision-failed").
				WithGeneration(5555).
				ToChildRevision(resources.MakeRevision).
				WithReadyConditionFalse(
					"MissingContainer",
					"It's the end of the world as we know it",
				).
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			Config("matching-revision-failed").
				WithGeneration(5555).
				WithLatestCreatedRevisionName("matching-revision-failed-05555").
				WithObservedGeneration(5555).
				WithReadyConditionFalse(
					"RevisionFailed",
					`Revision "matching-revision-failed-05555" failed with message: "It's the end of the world as we know it".`,
				).
				ToUpdateAction(),
		},
		Key: "default/matching-revision-failed",
	}, {
		Name: "reconcile revision matching generation (ready: bad)",
		Objects: []runtime.Object{
			Config("bad-condition").
				WithGeneration(5555).
				Build(),
			Config("bad-condition").
				WithGeneration(5555).
				ToChildRevision(resources.MakeRevision).
				WithReadyCondition("Bad").
				Build(),
		},
		WantErr: true,
		WantUpdates: []clientgotesting.UpdateActionImpl{
			Config("bad-condition").
				WithGeneration(5555).
				WithLatestCreatedRevisionName("bad-condition-05555").
				WithObservedGeneration(5555).
				WithReadyConditionUnknown().
				ToUpdateAction(),
		},
		Key: "default/bad-condition",
	}, {
		Name: "failure creating build",
		// We induce a failure creating a build
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "builds"),
		},
		Objects: []runtime.Object{
			Config("create-build-failure").
				WithGeneration(99998).
				WithBuild(buildSpec).
				Build(),
		},
		WantCreates: []metav1.Object{
			Config("create-build-failure").
				WithGeneration(99998).
				WithBuild(buildSpec).
				ToChildBuild(resources.MakeBuild),
			// No Revision gets created.
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			Config("create-build-failure").
				WithGeneration(99998).
				WithBuild(buildSpec).
				WithReadyConditionFalse(
					"RevisionFailed",
					`Revision creation failed with message: "inducing failure for create builds".`,
				).
				ToUpdateAction(),
		},
		Key: "default/create-build-failure",
	}, {
		Name: "failure creating revision",
		// We induce a failure creating a revision
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "revisions"),
		},
		Objects: []runtime.Object{
			Config("create-revision-failure").
				WithGeneration(99998).
				Build(),
		},
		WantCreates: []metav1.Object{
			Config("create-revision-failure").
				WithGeneration(99998).
				ToChildRevision(resources.MakeRevision).
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			Config("create-revision-failure").
				WithGeneration(99998).
				WithReadyConditionFalse(
					"RevisionFailed",
					`Revision creation failed with message: "inducing failure for create revisions".`,
				).
				ToUpdateAction(),
		},
		Key: "default/create-revision-failure",
	}, {
		Name: "failure updating configuration status",
		// Induce a failure updating the status of the configuration.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "configurations"),
		},
		Objects: []runtime.Object{
			Config("update-config-failure").
				WithGeneration(1234).
				Build(),
		},
		WantCreates: []metav1.Object{
			Config("update-config-failure").
				WithGeneration(1234).
				ToChildRevision(resources.MakeRevision).
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			Config("update-config-failure").
				WithGeneration(1234).
				WithLatestCreatedRevisionName("update-config-failure-01234").
				WithObservedGeneration(1234).
				WithReadyConditionUnknown().
				ToUpdateAction(),
		},
		Key: "default/update-config-failure",
	}, {
		Name: "failed revision recovers",
		Objects: []runtime.Object{
			Config("revision-recovers").
				WithGeneration(1337).
				WithLatestCreatedRevisionName("revision-recovers-01337").
				WithLatestReadyRevisionName("revision-recovers-01337").
				WithReadyConditionFalse(
					"RevisionFailed",
					`Revision "revision-recovers-01337" failed with message: "Weebles wobble, but they don't fall down".`,
				).
				Build(),
			Config("revision-recovers").
				WithGeneration(1337).
				ToChildRevision(resources.MakeRevision).
				WithReadyConditionTrue().
				Build(),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{
			Config("revision-recovers").
				WithGeneration(1337).
				WithLatestCreatedRevisionName("revision-recovers-01337").
				WithLatestReadyRevisionName("revision-recovers-01337").
				WithObservedGeneration(1337).
				WithReadyConditionTrue().
				ToUpdateAction(),
		},
		Key: "default/revision-recovers",
	}}

	table.Test(t, func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		return &Reconciler{
			Base:                reconciler.NewBase(opt, controllerAgentName),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
		}
	})
}
