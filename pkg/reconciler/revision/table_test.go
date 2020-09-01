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

package revision

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	caching "knative.dev/caching/pkg/apis/caching/v1alpha1"
	cachingclient "knative.dev/caching/pkg/client/injection/client"
	"knative.dev/networking/pkg/apis/networking"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	pkgreconciler "knative.dev/pkg/reconciler"
	tracingconfig "knative.dev/pkg/tracing/config"
	asv1a1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	defaultconfig "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	asconfig "knative.dev/serving/pkg/autoscaler/config"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	revisionreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/revision"
	"knative.dev/serving/pkg/reconciler/revision/config"
	"knative.dev/serving/pkg/reconciler/revision/resources"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing"
	. "knative.dev/serving/pkg/testing/v1"
)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name: "bad workqueue key",
		// Make sure Reconcile handles bad keys.
		Key: "too/many/parts",
	}, {
		Name: "key not found",
		// Make sure Reconcile handles good keys that don't exist.
		Key: "foo/not-found",
	}, {
		Name: "nop deletion reconcile",
		// Test that with a DeletionTimestamp we do nothing.
		Objects: []runtime.Object{
			Revision("foo", "delete-pending", WithRevisionDeletionTimestamp),
		},
		Key: "foo/delete-pending",
	}, {
		Name: "first revision reconciliation",
		// Test the simplest successful reconciliation flow.
		// We feed in a well formed Revision where none of its sub-resources exist,
		// and we expect it to create them and initialize the Revision's status.
		Objects: []runtime.Object{
			Revision("foo", "first-reconcile"),
		},
		WantCreates: []runtime.Object{
			// The first reconciliation of a Revision creates the following resources.
			pa("foo", "first-reconcile"),
			deploy(t, "foo", "first-reconcile"),
			image("foo", "first-reconcile"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "first-reconcile",
				// The first reconciliation Populates the following status properties.
				WithLogURL, allUnknownConditions, MarkDeploying("Deploying"),
				withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		Key: "foo/first-reconcile",
	}, {
		Name: "failure updating revision status",
		// This starts from the first reconciliation case above and induces a failure
		// updating the revision status.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "revisions"),
		},
		Objects: []runtime.Object{
			Revision("foo", "update-status-failure"),
			pa("foo", "update-status-failure"),
		},
		WantCreates: []runtime.Object{
			// We still see the following creates before the failure is induced.
			deploy(t, "foo", "update-status-failure"),
			image("foo", "update-status-failure"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "update-status-failure",
				// Despite failure, the following status properties are set.
				WithLogURL, allUnknownConditions, MarkDeploying("Deploying"),
				withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "Failed to update status for %q: %v",
				"update-status-failure", "inducing failure for update revisions"),
		},
		Key: "foo/update-status-failure",
	}, {
		Name: "failure creating pa",
		// This starts from the first reconciliation case above and induces a failure
		// creating the PA.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "podautoscalers"),
		},
		Objects: []runtime.Object{
			Revision("foo", "create-pa-failure"),
		},
		WantCreates: []runtime.Object{
			// We still see the following creates before the failure is induced.
			pa("foo", "create-pa-failure"),
			deploy(t, "foo", "create-pa-failure"),
			image("foo", "create-pa-failure"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "create-pa-failure",
				// Despite failure, the following status properties are set.
				WithLogURL, WithInitRevConditions,
				MarkDeploying("Deploying"), withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `failed to create PA "create-pa-failure": inducing failure for create podautoscalers`),
		},
		Key: "foo/create-pa-failure",
	}, {
		Name: "failure creating user deployment",
		// This starts from the first reconciliation case above and induces a failure
		// creating the user's deployment
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "deployments"),
		},
		Objects: []runtime.Object{
			Revision("foo", "create-user-deploy-failure"),
			pa("foo", "create-user-deploy-failure"),
		},
		WantCreates: []runtime.Object{
			// We still see the following creates before the failure is induced.
			deploy(t, "foo", "create-user-deploy-failure"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "create-user-deploy-failure",
				// Despite failure, the following status properties are set.
				WithLogURL, WithInitRevConditions,
				MarkDeploying("Deploying"), withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to create deployment "create-user-deploy-failure-deployment": inducing failure for create deployments`),
		},
		Key: "foo/create-user-deploy-failure",
	}, {
		Name: "stable revision reconciliation",
		// Test a simple stable reconciliation of an Active Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (immediately post-creation), and verify that no changes
		// are necessary.
		Objects: []runtime.Object{
			Revision("foo", "stable-reconcile", WithLogURL, allUnknownConditions,
				withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
			pa("foo", "stable-reconcile", WithReachabilityUnknown),

			deploy(t, "foo", "stable-reconcile"),
			image("foo", "stable-reconcile"),
		},
		// No changes are made to any objects.
		Key: "foo/stable-reconcile",
	}, {
		Name: "update deployment containers",
		// Test that we update a deployment with new containers when they disagree
		// with our desired spec.
		Objects: []runtime.Object{
			Revision("foo", "fix-containers",
				WithLogURL, allUnknownConditions, withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
			pa("foo", "fix-containers", WithReachabilityUnknown),
			changeContainers(deploy(t, "foo", "fix-containers")),
			image("foo", "fix-containers"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: deploy(t, "foo", "fix-containers"),
		}},
		Key: "foo/fix-containers",
	}, {
		Name: "failure updating deployment",
		// Test that we handle an error updating the deployment properly.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "deployments"),
		},
		Objects: []runtime.Object{
			Revision("foo", "failure-update-deploy",
				WithK8sServiceName("whateves"), WithLogURL, allUnknownConditions,
				withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
			pa("foo", "failure-update-deploy"),
			changeContainers(deploy(t, "foo", "failure-update-deploy")),
			image("foo", "failure-update-deploy"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: deploy(t, "foo", "failure-update-deploy"),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to update deployment "failure-update-deploy-deployment": inducing failure for update deployments`),
		},
		Key: "foo/failure-update-deploy",
	}, {
		Name: "deactivated revision is stable",
		// Test a simple stable reconciliation of an inactive Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (port-Reserve), and verify that no changes are necessary.
		Objects: []runtime.Object{
			Revision("foo", "stable-deactivation",
				WithLogURL, MarkRevisionReady,
				MarkInactive("NoTraffic", "This thing is inactive."),
				withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
			pa("foo", "stable-deactivation",
				WithNoTraffic("NoTraffic", "This thing is inactive."), WithReachabilityUnreachable,
				WithScaleTargetInitialized),
			deploy(t, "foo", "stable-deactivation"),
			image("foo", "stable-deactivation"),
		},
		Key: "foo/stable-deactivation",
	}, {
		Name: "pa is ready",
		Objects: []runtime.Object{
			Revision("foo", "pa-ready",
				WithK8sServiceName("old-stuff"), WithLogURL, allUnknownConditions),
			pa("foo", "pa-ready", WithPASKSReady, WithTraffic,
				WithScaleTargetInitialized, WithPAStatusService("new-stuff"), WithReachabilityUnknown),
			deploy(t, "foo", "pa-ready"),
			image("foo", "pa-ready"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "pa-ready", WithK8sServiceName("new-stuff"),
				WithLogURL,
				// When the endpoint and pa are ready, then we will see the
				// Revision become ready.
				MarkRevisionReady, withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "RevisionReady", "Revision becomes ready upon all resources being ready"),
		},
		Key: "foo/pa-ready",
	}, {
		Name: "pa not ready",
		// Test propagating the pa not ready status to the Revision.
		Objects: []runtime.Object{
			Revision("foo", "pa-not-ready",
				WithK8sServiceName("somebody-told-me"), WithLogURL,
				MarkRevisionReady, WithRevisionObservedGeneration(1)),
			pa("foo", "pa-not-ready",
				WithPAStatusService("its-not-confidential"),
				WithBufferedTraffic,
				WithReachabilityUnreachable),
			readyDeploy(deploy(t, "foo", "pa-not-ready")),
			image("foo", "pa-not-ready"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "pa-not-ready",
				WithLogURL, MarkRevisionReady, withDefaultContainerStatuses(),
				WithK8sServiceName("its-not-confidential"),
				// When we reconcile a ready state and our pa is in an activating
				// state, we should see the following mutation.
				MarkActivating("Queued", "Requests to the target are being buffered as resources are provisioned."),
				WithRevisionObservedGeneration(1),
			),
		}},
		Key: "foo/pa-not-ready",
	}, {
		Name: "pa inactive",
		// Test propagating the inactivity signal from the pa to the Revision.
		Objects: []runtime.Object{
			Revision("foo", "pa-inactive",
				WithK8sServiceName("something-in-the-way"), WithLogURL,
				MarkRevisionReady, WithRevisionObservedGeneration(1)),
			pa("foo", "pa-inactive",
				WithNoTraffic("NoTraffic", "This thing is inactive."),
				WithScaleTargetInitialized,
				WithReachabilityUnreachable),
			readyDeploy(deploy(t, "foo", "pa-inactive")),
			image("foo", "pa-inactive"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "pa-inactive",
				WithLogURL, MarkRevisionReady, withDefaultContainerStatuses(),
				// When we reconcile an "all ready" revision when the PA
				// is inactive, we should see the following change.
				MarkInactive("NoTraffic", "This thing is inactive."), WithRevisionObservedGeneration(1)),
		}},
		Key: "foo/pa-inactive",
	}, {
		Name: "pa inactive II",
		// Test propagating the inactivity signal from the pa to the Revision.
		Objects: []runtime.Object{
			Revision("foo", "pa-inactive",
				WithK8sServiceName("something-in-the-way"), WithLogURL,
				WithRevisionObservedGeneration(1)),
			pa("foo", "pa-inactive",
				WithNoTraffic("NoTraffic", "This thing is inactive."),
				WithPAStatusService("pa-inactive")),
			readyDeploy(deploy(t, "foo", "pa-inactive")),
			image("foo", "pa-inactive"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "pa-inactive",
				WithLogURL, withDefaultContainerStatuses(), MarkDeploying(""),
				// When we reconcile an "all ready" revision when the PA
				// is inactive, we should see the following change.
				MarkInactive("NoTraffic", "This thing is inactive."), WithRevisionObservedGeneration(1),
				MarkResourcesUnavailable(v1.ReasonProgressDeadlineExceeded,
					"Initial scale was never achieved"), WithK8sServiceName("pa-inactive")),
		}},
		Key: "foo/pa-inactive",
	}, {
		Name: "pa is not ready with initial scale zero, but ServiceName still empty, so not marking resources available false",
		Objects: []runtime.Object{
			Revision("foo", "pa-inactive", allUnknownConditions,
				WithK8sServiceName("something-in-the-way"), WithLogURL,
				WithRevisionObservedGeneration(1)),
			pa("foo", "pa-inactive",
				WithNoTraffic("NoTraffic", "This thing is inactive."), WithPAStatusService("")),
			readyDeploy(deploy(t, "foo", "pa-inactive")),
			image("foo", "pa-inactive"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			// We should not mark resources unavailable if ServiceName is empty
			Object: Revision("foo", "pa-inactive",
				WithLogURL, withDefaultContainerStatuses(), allUnknownConditions,
				MarkInactive("NoTraffic", "This thing is inactive."),
				WithRevisionObservedGeneration(1)),
		}},
		Key: "foo/pa-inactive",
	}, {
		Name: "pa inactive, but has service",
		// Test propagating the inactivity signal from the pa to the Revision.
		// But propagate the service name.
		Objects: []runtime.Object{
			Revision("foo", "pa-inactive",
				WithK8sServiceName("here-comes-the-sun"), WithLogURL,
				MarkRevisionReady, WithRevisionObservedGeneration(1)),
			pa("foo", "pa-inactive",
				WithNoTraffic("NoTraffic", "This thing is inactive."),
				WithPAStatusService("pa-inactive-svc"),
				WithScaleTargetInitialized,
				WithReachabilityUnreachable),
			readyDeploy(deploy(t, "foo", "pa-inactive")),
			image("foo", "pa-inactive"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "pa-inactive",
				WithLogURL, MarkRevisionReady,
				WithK8sServiceName("pa-inactive-svc"),
				// When we reconcile an "all ready" revision when the PA
				// is inactive, we should see the following change.
				MarkInactive("NoTraffic", "This thing is inactive."),
				withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		Key: "foo/pa-inactive",
	}, {
		Name: "mutated pa gets fixed",
		// This test validates, that when users mess with the pa directly
		// we bring it back to the required shape.
		// Protocol type is the only thing that can be changed on PA
		Objects: []runtime.Object{
			Revision("foo", "fix-mutated-pa",
				WithK8sServiceName("ill-follow-the-sun"), WithLogURL, MarkRevisionReady,
				WithRevisionLabel(serving.RouteLabelKey, "foo")),
			pa("foo", "fix-mutated-pa", WithProtocolType(networking.ProtocolH2C),
				WithTraffic, WithPASKSReady, WithScaleTargetInitialized, WithReachabilityReachable,
				WithPAStatusService("fix-mutated-pa")),
			deploy(t, "foo", "fix-mutated-pa"),
			image("foo", "fix-mutated-pa"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "fix-mutated-pa",
				WithLogURL, allUnknownConditions,
				// When our reconciliation has to change the service
				// we should see the following mutations to status.
				WithK8sServiceName("fix-mutated-pa"),
				WithRevisionLabel(serving.RouteLabelKey, "foo"), WithLogURL, MarkRevisionReady,
				withDefaultContainerStatuses()),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: pa("foo", "fix-mutated-pa", WithPASKSReady,
				WithTraffic, WithScaleTargetInitialized,
				WithPAStatusService("fix-mutated-pa"), WithReachabilityReachable),
		}},
		Key: "foo/fix-mutated-pa",
	}, {
		Name: "mutated pa gets error during the fix",
		// Same as above, but will fail during the update.
		Objects: []runtime.Object{
			Revision("foo", "fix-mutated-pa-fail",
				WithK8sServiceName("some-old-stuff"),
				WithLogURL, allUnknownConditions, withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
			pa("foo", "fix-mutated-pa-fail", WithProtocolType(networking.ProtocolH2C), WithReachabilityUnknown),
			deploy(t, "foo", "fix-mutated-pa-fail"),
			image("foo", "fix-mutated-pa-fail"),
		},
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "podautoscalers"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: pa("foo", "fix-mutated-pa-fail", WithReachabilityUnknown),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `failed to update PA "fix-mutated-pa-fail": inducing failure for update podautoscalers`),
		},
		Key: "foo/fix-mutated-pa-fail",
	}, {
		Name: "surface deployment timeout",
		// Test the propagation of ProgressDeadlineExceeded from Deployment.
		// This initializes the world to the stable state after its first reconcile,
		// but changes the user deployment to have a ProgressDeadlineExceeded
		// condition.  It then verifies that Reconcile propagates this into the
		// status of the Revision.
		Objects: []runtime.Object{
			Revision("foo", "deploy-timeout",
				WithK8sServiceName("the-taxman"), WithLogURL, MarkActive),
			pa("foo", "deploy-timeout"), // pa can't be ready since deployment times out.
			timeoutDeploy(deploy(t, "foo", "deploy-timeout"), "I timed out!"),
			image("foo", "deploy-timeout"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "deploy-timeout",
				WithLogURL, allUnknownConditions,
				// When the revision is reconciled after a Deployment has
				// timed out, we should see it marked with the PDE state.
				MarkProgressDeadlineExceeded("I timed out!"), withDefaultContainerStatuses(),
				WithRevisionObservedGeneration(1)),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: pa("foo", "deploy-timeout", WithReachabilityUnreachable),
		}},
		Key: "foo/deploy-timeout",
	}, {
		Name: "surface replica failure",
		// Test the propagation of FailedCreate from Deployment.
		// This initializes the world to the stable state after its first reconcile,
		// but changes the user deployment to have a FailedCreate condition.
		// It then verifies that Reconcile propagates this into the status of the Revision.
		Objects: []runtime.Object{
			Revision("foo", "deploy-replica-failure",
				WithK8sServiceName("the-taxman"), WithLogURL, MarkActive),
			pa("foo", "deploy-replica-failure"),
			replicaFailureDeploy(deploy(t, "foo", "deploy-replica-failure"), "I replica failed!"),
			image("foo", "deploy-replica-failure"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "deploy-replica-failure",
				WithLogURL, allUnknownConditions,
				// When the revision is reconciled after a Deployment has
				// timed out, we should see it marked with the FailedCreate state.
				MarkResourcesUnavailable("FailedCreate", "I replica failed!"),
				withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: pa("foo", "deploy-replica-failure", WithReachabilityUnreachable),
		}},
		Key: "foo/deploy-replica-failure",
	}, {
		Name: "surface ImagePullBackoff",
		// Test the propagation of ImagePullBackoff from user container.
		Objects: []runtime.Object{
			Revision("foo", "pull-backoff",
				WithK8sServiceName("the-taxman"), WithLogURL, MarkActivating("Deploying", "")),
			pa("foo", "pull-backoff"), // pa can't be ready since deployment times out.
			pod(t, "foo", "pull-backoff", WithWaitingContainer("pull-backoff", "ImagePullBackoff", "can't pull it")),
			timeoutDeploy(deploy(t, "foo", "pull-backoff"), "Timed out!"),
			image("foo", "pull-backoff"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "pull-backoff",
				WithLogURL, allUnknownConditions,
				MarkResourcesUnavailable("ImagePullBackoff", "can't pull it"), withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: pa("foo", "pull-backoff", WithReachabilityUnreachable),
		}},
		Key: "foo/pull-backoff",
	}, {
		Name: "surface pod errors",
		// Test the propagation of the termination state of a Pod into the revision.
		// This initializes the world to the stable state after its first reconcile,
		// but changes the user deployment to have a failing pod. It then verifies
		// that Reconcile propagates this into the status of the Revision.
		Objects: []runtime.Object{
			Revision("foo", "pod-error",
				WithK8sServiceName("a-pod-error"), WithLogURL, allUnknownConditions, MarkActive),
			pa("foo", "pod-error"), // PA can't be ready, since no traffic.
			pod(t, "foo", "pod-error", WithFailingContainer("pod-error", 5, "I failed man!")),
			deploy(t, "foo", "pod-error"),
			image("foo", "pod-error"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "pod-error",
				WithLogURL, allUnknownConditions, MarkContainerExiting(5,
					v1.RevisionContainerExitingMessage("I failed man!")), withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: pa("foo", "pod-error", WithReachabilityUnreachable),
		}},
		Key: "foo/pod-error",
	}, {
		Name: "surface pod schedule errors",
		// Test the propagation of the scheduling errors of Pod into the revision.
		// This initializes the world to unschedule pod. It then verifies
		// that Reconcile propagates this into the status of the Revision.
		Objects: []runtime.Object{
			Revision("foo", "pod-schedule-error",
				WithK8sServiceName("a-pod-schedule-error"), WithLogURL, allUnknownConditions, MarkActive),
			pa("foo", "pod-schedule-error"), // PA can't be ready, since no traffic.
			pod(t, "foo", "pod-schedule-error", WithUnschedulableContainer("Insufficient energy", "Unschedulable")),
			deploy(t, "foo", "pod-schedule-error"),
			image("foo", "pod-schedule-error"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "pod-schedule-error",
				WithLogURL, allUnknownConditions, MarkResourcesUnavailable("Insufficient energy",
					"Unschedulable"), withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: pa("foo", "pod-schedule-error", WithReachabilityUnreachable),
		}},
		Key: "foo/pod-schedule-error",
	}, {
		Name: "ready steady state",
		// Test the transition that Reconcile makes when Endpoints become ready on the
		// SKS owned services, which is signalled by pa having service name.
		// This puts the world into the stable post-reconcile state for an Active
		// Revision.  It then creates an Endpoints resource with active subsets.
		// This signal should make our Reconcile mark the Revision as Ready.
		Objects: []runtime.Object{
			Revision("foo", "steady-ready", WithK8sServiceName("very-steady"), WithLogURL),
			pa("foo", "steady-ready", WithPASKSReady, WithTraffic,
				WithScaleTargetInitialized, WithPAStatusService("steadier-even")),
			deploy(t, "foo", "steady-ready"),
			image("foo", "steady-ready"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "steady-ready", WithK8sServiceName("steadier-even"), WithLogURL,
				// All resources are ready to go, we should see the revision being
				// marked ready
				MarkRevisionReady, withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "RevisionReady", "Revision becomes ready upon all resources being ready"),
		},
		Key: "foo/steady-ready",
	}, {
		Name:    "lost pa owner ref",
		WantErr: true,
		Objects: []runtime.Object{
			Revision("foo", "missing-owners", WithK8sServiceName("lesser-revision"), WithLogURL,
				MarkRevisionReady),
			pa("foo", "missing-owners", WithTraffic, WithPodAutoscalerOwnersRemoved),
			deploy(t, "foo", "missing-owners"),
			image("foo", "missing-owners"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "missing-owners", WithK8sServiceName("lesser-revision"), WithLogURL,
				MarkRevisionReady,
				// When we're missing the OwnerRef for PodAutoscaler we see this update.
				MarkResourceNotOwned("PodAutoscaler", "missing-owners"), withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `revision: "missing-owners" does not own PodAutoscaler: "missing-owners"`),
		},
		Key: "foo/missing-owners",
	}, {
		Name:    "lost deployment owner ref",
		WantErr: true,
		Objects: []runtime.Object{
			Revision("foo", "missing-owners", WithK8sServiceName("youre-gonna-lose"), WithLogURL,
				MarkRevisionReady),
			pa("foo", "missing-owners", WithTraffic),
			noOwner(deploy(t, "foo", "missing-owners")),
			image("foo", "missing-owners"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "missing-owners", WithK8sServiceName("youre-gonna-lose"), WithLogURL,
				MarkRevisionReady,
				// When we're missing the OwnerRef for Deployment we see this update.
				MarkResourceNotOwned("Deployment", "missing-owners-deployment"), withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `revision: "missing-owners" does not own Deployment: "missing-owners-deployment"`),
		},
		Key: "foo/missing-owners",
	}, {
		Name: "image pull secrets",
		// This test case tests that the image pull secrets from revision propagate to deployment and image
		Objects: []runtime.Object{
			Revision("foo", "image-pull-secrets", WithImagePullSecrets("foo-secret")),
		},
		WantCreates: []runtime.Object{
			pa("foo", "image-pull-secrets"),
			deployImagePullSecrets(deploy(t, "foo", "image-pull-secrets"), "foo-secret"),
			imagePullSecrets(image("foo", "image-pull-secrets"), "foo-secret"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: Revision("foo", "image-pull-secrets",
				WithImagePullSecrets("foo-secret"),
				WithLogURL, allUnknownConditions, MarkDeploying("Deploying"), withDefaultContainerStatuses(), WithRevisionObservedGeneration(1)),
		}},
		Key: "foo/image-pull-secrets",
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		r := &Reconciler{
			kubeclient:    kubeclient.Get(ctx),
			client:        servingclient.Get(ctx),
			cachingclient: cachingclient.Get(ctx),

			podAutoscalerLister: listers.GetPodAutoscalerLister(),
			imageLister:         listers.GetImageLister(),
			deploymentLister:    listers.GetDeploymentLister(),
			resolver:            &nopResolver{},
		}

		return revisionreconciler.NewReconciler(ctx, logging.FromContext(ctx), servingclient.Get(ctx),
			listers.GetRevisionLister(), controller.GetEventRecorder(ctx), r,
			controller.Options{
				ConfigStore: &testConfigStore{
					config: ReconcilerTestConfig(),
				},
			})
	}))
}

func readyDeploy(deploy *appsv1.Deployment) *appsv1.Deployment {
	deploy.Status.Conditions = []appsv1.DeploymentCondition{{
		Type:   appsv1.DeploymentProgressing,
		Status: corev1.ConditionTrue,
	}}
	return deploy
}

func timeoutDeploy(deploy *appsv1.Deployment, message string) *appsv1.Deployment {
	deploy.Status.Conditions = []appsv1.DeploymentCondition{{
		Type:    appsv1.DeploymentProgressing,
		Status:  corev1.ConditionFalse,
		Reason:  v1.ReasonProgressDeadlineExceeded,
		Message: message,
	}}
	return deploy
}

func replicaFailureDeploy(deploy *appsv1.Deployment, message string) *appsv1.Deployment {
	deploy.Status.Conditions = []appsv1.DeploymentCondition{{
		Type:    appsv1.DeploymentReplicaFailure,
		Status:  corev1.ConditionTrue,
		Reason:  "FailedCreate",
		Message: message,
	}}
	return deploy
}

func noOwner(deploy *appsv1.Deployment) *appsv1.Deployment {
	deploy.OwnerReferences = nil
	return deploy
}

func deployImagePullSecrets(deploy *appsv1.Deployment, secretName string) *appsv1.Deployment {
	deploy.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{
		Name: secretName,
	}}
	return deploy
}

func imagePullSecrets(Revision *caching.Image, secretName string) *caching.Image {
	Revision.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{
		Name: secretName,
	}}
	return Revision
}

func changeContainers(deploy *appsv1.Deployment) *appsv1.Deployment {
	podSpec := deploy.Spec.Template.Spec
	for i := range podSpec.Containers {
		podSpec.Containers[i].Image = "asdf"
	}
	return deploy
}

func withDefaultContainerStatuses() RevisionOption {
	return func(r *v1.Revision) {
		r.Status.ContainerStatuses = []v1.ContainerStatuses{{
			Name:        r.Name,
			ImageDigest: "",
		}}
	}
}

// TODO(mattmoor): Come up with a better name for this.
func allUnknownConditions(r *v1.Revision) {
	WithInitRevConditions(r)
	MarkDeploying("")(r)
	MarkActivating("Deploying", "")(r)
}

type configOption func(*config.Config)

func deploy(t *testing.T, namespace, name string, opts ...interface{}) *appsv1.Deployment {
	t.Helper()
	cfg := ReconcilerTestConfig()

	for _, opt := range opts {
		if configOpt, ok := opt.(configOption); ok {
			configOpt(cfg)
		}
	}

	rev := Revision(namespace, name)

	for _, opt := range opts {
		if revOpt, ok := opt.(RevisionOption); ok {
			revOpt(rev)
		}
	}

	// Do this here instead of in `rev` itself to ensure that we populate defaults
	// before calling MakeDeployment within Reconcile.
	rev.SetDefaults(context.Background())
	deployment, err := resources.MakeDeployment(rev, cfg.Logging, cfg.Tracing, cfg.Network,
		cfg.Observability, cfg.Deployment, cfg.Autoscaler)
	if err != nil {
		t.Fatal("failed to create deployment")
	}
	return deployment
}

func image(namespace, name string, co ...configOption) *caching.Image {
	config := ReconcilerTestConfig()
	for _, opt := range co {
		opt(config)
	}

	return resources.MakeImageCache(Revision(namespace, name), name, "")
}

func pa(namespace, name string, ko ...PodAutoscalerOption) *asv1a1.PodAutoscaler {
	rev := Revision(namespace, name)
	k := resources.MakePA(rev)

	for _, opt := range ko {
		opt(k)
	}
	return k
}

func pod(t *testing.T, namespace, name string, po ...PodOption) *corev1.Pod {
	t.Helper()
	deploy := deploy(t, namespace, name)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    deploy.Labels,
		},
	}

	for _, opt := range po {
		opt(pod)
	}
	return pod
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

var _ pkgreconciler.ConfigStore = (*testConfigStore)(nil)

func ReconcilerTestConfig() *config.Config {
	return &config.Config{
		Deployment: testDeploymentConfig(),
		Observability: &metrics.ObservabilityConfig{
			LoggingURLTemplate: "http://logger.io/${REVISION_UID}",
		},
		Logging:  &logging.Config{},
		Tracing:  &tracingconfig.Config{},
		Defaults: &defaultconfig.Defaults{},
		Autoscaler: &asconfig.Config{
			InitialScale:          1,
			AllowZeroInitialScale: false,
		},
	}
}
