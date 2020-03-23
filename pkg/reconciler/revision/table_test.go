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
	"strconv"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	caching "knative.dev/caching/pkg/apis/caching/v1alpha1"
	cachingclient "knative.dev/caching/pkg/client/injection/client"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/ptr"
	pkgreconciler "knative.dev/pkg/reconciler"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/serving/pkg/apis/autoscaling"
	asv1a1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	defaultconfig "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/networking"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
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
	defaultCtx := autoscalerConfigCtx(false, 1)
	table := TableTest{{
		Name: "bad workqueue key",
		// Make sure Reconcile handles bad keys.
		Key: "too/many/parts",
		Ctx: defaultCtx,
	}, {
		Name: "key not found",
		// Make sure Reconcile handles good keys that don't exist.
		Key: "foo/not-found",
		Ctx: defaultCtx,
	}, {
		Name: "nop deletion reconcile",
		Ctx:  defaultCtx,
		// Test that with a DeletionTimestamp we do nothing.
		Objects: []runtime.Object{
			rev("foo", "delete-pending", WithRevisionDeletionTimestamp),
		},
		Key: "foo/delete-pending",
	}, {
		Name: "first revision reconciliation",
		Ctx:  defaultCtx,
		// Test the simplest successful reconciliation flow.
		// We feed in a well formed Revision where none of its sub-resources exist,
		// and we expect it to create them and initialize the Revision's status.
		Objects: []runtime.Object{
			rev("foo", "first-reconcile"),
		},
		WantCreates: []runtime.Object{
			// The first reconciliation of a Revision creates the following resources.
			pa("foo", "first-reconcile"),
			deploy(t, "foo", "first-reconcile"),
			image("foo", "first-reconcile"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "first-reconcile",
				// The first reconciliation Populates the following status properties.
				WithLogURL, AllUnknownConditions, MarkDeploying("Deploying"), WithContainerStatuses([]v1.ContainerStatuses{
					{Name: "first-reconcile"},
				})),
		}},
		Key: "foo/first-reconcile",
	}, {
		Name: "failure updating revision status",
		Ctx:  defaultCtx,
		// This starts from the first reconciliation case above and induces a failure
		// updating the revision status.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "revisions"),
		},
		Objects: []runtime.Object{
			rev("foo", "update-status-failure"),
			pa("foo", "update-status-failure"),
		},
		WantCreates: []runtime.Object{
			// We still see the following creates before the failure is induced.
			deploy(t, "foo", "update-status-failure"),
			image("foo", "update-status-failure"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "update-status-failure",
				// Despite failure, the following status properties are set.
				WithLogURL, AllUnknownConditions, MarkDeploying("Deploying"), WithContainerStatuses([]v1.ContainerStatuses{
					{Name: "update-status-failure"},
				})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "Failed to update status for %q: %v",
				"update-status-failure", "inducing failure for update revisions"),
		},
		Key: "foo/update-status-failure",
	}, {
		Name: "failure creating pa",
		Ctx:  defaultCtx,
		// This starts from the first reconciliation case above and induces a failure
		// creating the PA.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "podautoscalers"),
		},
		Objects: []runtime.Object{
			rev("foo", "create-pa-failure"),
		},
		WantCreates: []runtime.Object{
			// We still see the following creates before the failure is induced.
			pa("foo", "create-pa-failure"),
			deploy(t, "foo", "create-pa-failure"),
			image("foo", "create-pa-failure"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "create-pa-failure",
				// Despite failure, the following status properties are set.
				WithLogURL, WithInitRevConditions,
				MarkDeploying("Deploying"), WithContainerStatuses([]v1.ContainerStatuses{{Name: "create-pa-failure"}})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `failed to create PA "create-pa-failure": inducing failure for create podautoscalers`),
		},
		Key: "foo/create-pa-failure",
	}, {
		Name: "failure creating user deployment",
		Ctx:  defaultCtx,
		// This starts from the first reconciliation case above and induces a failure
		// creating the user's deployment
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "deployments"),
		},
		Objects: []runtime.Object{
			rev("foo", "create-user-deploy-failure"),
			pa("foo", "create-user-deploy-failure"),
		},
		WantCreates: []runtime.Object{
			// We still see the following creates before the failure is induced.
			deploy(t, "foo", "create-user-deploy-failure"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "create-user-deploy-failure",
				// Despite failure, the following status properties are set.
				WithLogURL, WithInitRevConditions,
				MarkDeploying("Deploying"), WithContainerStatuses([]v1.ContainerStatuses{{Name: "create-user-deploy-failure"}})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to create deployment "create-user-deploy-failure-deployment": inducing failure for create deployments`),
		},
		Key: "foo/create-user-deploy-failure",
	}, {
		Name: "stable revision reconciliation",
		Ctx:  defaultCtx,
		// Test a simple stable reconciliation of an Active Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (immediately post-creation), and verify that no changes
		// are necessary.
		Objects: []runtime.Object{
			rev("foo", "stable-reconcile", WithLogURL, AllUnknownConditions,
				WithContainerStatuses([]v1.ContainerStatuses{{Name: "stable-reconcile"}})),
			pa("foo", "stable-reconcile", WithReachability(asv1a1.ReachabilityUnknown)),

			deploy(t, "foo", "stable-reconcile"),
			image("foo", "stable-reconcile"),
		},
		// No changes are made to any objects.
		Key: "foo/stable-reconcile",
	}, {
		Name: "update deployment containers",
		Ctx:  defaultCtx,
		// Test that we update a deployment with new containers when they disagree
		// with our desired spec.
		Objects: []runtime.Object{
			rev("foo", "fix-containers",
				WithLogURL, AllUnknownConditions, WithContainerStatuses([]v1.ContainerStatuses{{Name: "fix-containers"}})),
			pa("foo", "fix-containers", WithReachability(asv1a1.ReachabilityUnknown)),
			changeContainers(deploy(t, "foo", "fix-containers")),
			image("foo", "fix-containers"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: deploy(t, "foo", "fix-containers"),
		}},
		Key: "foo/fix-containers",
	}, {
		Name: "failure updating deployment",
		Ctx:  defaultCtx,
		// Test that we handle an error updating the deployment properly.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "deployments"),
		},
		Objects: []runtime.Object{
			rev("foo", "failure-update-deploy",
				withK8sServiceName("whateves"), WithLogURL, AllUnknownConditions, WithContainerStatuses([]v1.ContainerStatuses{{Name: "failure-update-deploy"}})),
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
		Ctx:  defaultCtx,
		// Test a simple stable reconciliation of an inactive Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (port-Reserve), and verify that no changes are necessary.
		Objects: []runtime.Object{
			rev("foo", "stable-deactivation",
				WithLogURL, MarkRevisionReady,
				MarkInactive("NoTraffic", "This thing is inactive."), WithContainerStatuses([]v1.ContainerStatuses{{Name: "stable-deactivation"}})),
			pa("foo", "stable-deactivation",
				WithNoTraffic("NoTraffic", "This thing is inactive.")),
			deploy(t, "foo", "stable-deactivation"),
			image("foo", "stable-deactivation"),
		},
		Key: "foo/stable-deactivation",
	}, {
		Name: "pa is ready",
		Ctx:  defaultCtx,
		Objects: []runtime.Object{
			rev("foo", "pa-ready",
				withK8sServiceName("old-stuff"), WithLogURL, AllUnknownConditions),
			pa("foo", "pa-ready", WithTraffic, WithPAStatusService("new-stuff"), WithReachability(asv1a1.ReachabilityUnknown)),
			deploy(t, "foo", "pa-ready"),
			image("foo", "pa-ready"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "pa-ready", withK8sServiceName("new-stuff"),
				WithLogURL,
				// When the endpoint and pa are ready, then we will see the
				// Revision become ready.
				MarkRevisionReady, WithContainerStatuses([]v1.ContainerStatuses{{Name: "pa-ready"}})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "RevisionReady", "Revision becomes ready upon all resources being ready"),
		},
		Key: "foo/pa-ready",
	}, {
		Name: "pa not ready",
		Ctx:  defaultCtx,
		// Test propagating the pa not ready status to the Revision.
		Objects: []runtime.Object{
			rev("foo", "pa-not-ready",
				withK8sServiceName("somebody-told-me"), WithLogURL,
				MarkRevisionReady),
			pa("foo", "pa-not-ready",
				WithPAStatusService("its-not-confidential"),
				WithBufferedTraffic("Something", "This is something longer")),
			readyDeploy(deploy(t, "foo", "pa-not-ready")),
			image("foo", "pa-not-ready"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "pa-not-ready",
				WithLogURL, MarkRevisionReady, WithContainerStatuses([]v1.ContainerStatuses{{Name: "pa-not-ready"}}),
				withK8sServiceName("its-not-confidential"),
				// When we reconcile a ready state and our pa is in an activating
				// state, we should see the following mutation.
				MarkActivating("Something", "This is something longer"),
			),
		}},
		Key: "foo/pa-not-ready",
	}, {
		Name: "pa inactive",
		Ctx:  defaultCtx,
		// Test propagating the inactivity signal from the pa to the Revision.
		Objects: []runtime.Object{
			rev("foo", "pa-inactive",
				withK8sServiceName("something-in-the-way"), WithLogURL, MarkRevisionReady),
			pa("foo", "pa-inactive",
				WithNoTraffic("NoTraffic", "This thing is inactive.")),
			readyDeploy(deploy(t, "foo", "pa-inactive")),
			image("foo", "pa-inactive"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "pa-inactive",
				WithLogURL, MarkRevisionReady, WithContainerStatuses([]v1.ContainerStatuses{{Name: "pa-inactive"}}),
				// When we reconcile an "all ready" revision when the PA
				// is inactive, we should see the following change.
				MarkInactive("NoTraffic", "This thing is inactive.")),
		}},
		Key: "foo/pa-inactive",
	}, {
		Name: "pa inactive, but has service",
		Ctx:  defaultCtx,
		// Test propagating the inactivity signal from the pa to the Revision.
		// But propagate the service name.
		Objects: []runtime.Object{
			rev("foo", "pa-inactive",
				withK8sServiceName("here-comes-the-sun"), WithLogURL, MarkRevisionReady),
			pa("foo", "pa-inactive",
				WithNoTraffic("NoTraffic", "This thing is inactive."),
				WithPAStatusService("pa-inactive-svc")),
			readyDeploy(deploy(t, "foo", "pa-inactive")),
			image("foo", "pa-inactive"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "pa-inactive",
				WithLogURL, MarkRevisionReady,
				withK8sServiceName("pa-inactive-svc"),
				// When we reconcile an "all ready" revision when the PA
				// is inactive, we should see the following change.
				MarkInactive("NoTraffic", "This thing is inactive."), WithContainerStatuses([]v1.ContainerStatuses{{Name: "pa-inactive"}})),
		}},
		Key: "foo/pa-inactive",
	}, {
		Name: "mutated pa gets fixed",
		Ctx:  defaultCtx,
		// This test validates, that when users mess with the pa directly
		// we bring it back to the required shape.
		// Protocol type is the only thing that can be changed on PA
		Objects: []runtime.Object{
			rev("foo", "fix-mutated-pa",
				withK8sServiceName("ill-follow-the-sun"), WithLogURL, MarkRevisionReady),
			pa("foo", "fix-mutated-pa", WithProtocolType(networking.ProtocolH2C),
				WithTraffic, WithPAStatusService("fix-mutated-pa")),
			deploy(t, "foo", "fix-mutated-pa"),
			image("foo", "fix-mutated-pa"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "fix-mutated-pa",
				WithLogURL, AllUnknownConditions,
				// When our reconciliation has to change the service
				// we should see the following mutations to status.
				withK8sServiceName("fix-mutated-pa"), WithLogURL, MarkRevisionReady, WithContainerStatuses([]v1.ContainerStatuses{{Name: "fix-mutated-pa"}})),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: pa("foo", "fix-mutated-pa", WithTraffic,
				WithPAStatusService("fix-mutated-pa")),
		}},
		Key: "foo/fix-mutated-pa",
	}, {
		Name: "mutated pa gets error during the fix",
		Ctx:  defaultCtx,
		// Same as above, but will fail during the update.
		Objects: []runtime.Object{
			rev("foo", "fix-mutated-pa-fail",
				withK8sServiceName("some-old-stuff"),
				WithLogURL, AllUnknownConditions, WithContainerStatuses([]v1.ContainerStatuses{{Name: "fix-mutated-pa-fail"}})),
			pa("foo", "fix-mutated-pa-fail", WithProtocolType(networking.ProtocolH2C), WithReachability(asv1a1.ReachabilityUnknown)),
			deploy(t, "foo", "fix-mutated-pa-fail"),
			image("foo", "fix-mutated-pa-fail"),
		},
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "podautoscalers"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: pa("foo", "fix-mutated-pa-fail", WithReachability(asv1a1.ReachabilityUnknown)),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `failed to update PA "fix-mutated-pa-fail": inducing failure for update podautoscalers`),
		},
		Key: "foo/fix-mutated-pa-fail",
	}, {
		Name: "surface deployment timeout",
		Ctx:  defaultCtx,
		// Test the propagation of ProgressDeadlineExceeded from Deployment.
		// This initializes the world to the stable state after its first reconcile,
		// but changes the user deployment to have a ProgressDeadlineExceeded
		// condition.  It then verifies that Reconcile propagates this into the
		// status of the Revision.
		Objects: []runtime.Object{
			rev("foo", "deploy-timeout",
				withK8sServiceName("the-taxman"), WithLogURL, MarkActive),
			pa("foo", "deploy-timeout"), // pa can't be ready since deployment times out.
			timeoutDeploy(deploy(t, "foo", "deploy-timeout"), "I timed out!"),
			image("foo", "deploy-timeout"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "deploy-timeout",
				WithLogURL, AllUnknownConditions,
				// When the revision is reconciled after a Deployment has
				// timed out, we should see it marked with the PDE state.
				MarkProgressDeadlineExceeded("I timed out!"), WithContainerStatuses([]v1.ContainerStatuses{{Name: "deploy-timeout"}})),
		}},
		Key: "foo/deploy-timeout",
	}, {
		Name: "surface replica failure",
		Ctx:  defaultCtx,
		// Test the propagation of FailedCreate from Deployment.
		// This initializes the world to the stable state after its first reconcile,
		// but changes the user deployment to have a FailedCreate condition.
		// It then verifies that Reconcile propagates this into the status of the Revision.
		Objects: []runtime.Object{
			rev("foo", "deploy-replica-failure",
				withK8sServiceName("the-taxman"), WithLogURL, MarkActive),
			pa("foo", "deploy-replica-failure"),
			replicaFailureDeploy(deploy(t, "foo", "deploy-replica-failure"), "I replica failed!"),
			image("foo", "deploy-replica-failure"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "deploy-replica-failure",
				WithLogURL, AllUnknownConditions,
				// When the revision is reconciled after a Deployment has
				// timed out, we should see it marked with the FailedCreate state.
				MarkResourcesUnavailable("FailedCreate", "I replica failed!"), WithContainerStatuses([]v1.ContainerStatuses{{Name: "deploy-replica-failure"}})),
		}},
		Key: "foo/deploy-replica-failure",
	}, {
		Name: "surface ImagePullBackoff",
		Ctx:  defaultCtx,
		// Test the propagation of ImagePullBackoff from user container.
		Objects: []runtime.Object{
			rev("foo", "pull-backoff",
				withK8sServiceName("the-taxman"), WithLogURL, MarkActivating("Deploying", ""), WithContainerStatuses([]v1.ContainerStatuses{{Name: "pull-backoff"}})),
			pa("foo", "pull-backoff", WithReachability(asv1a1.ReachabilityUnknown)), // pa can't be ready since deployment times out.
			pod(t, "foo", "pull-backoff", WithWaitingContainer("pull-backoff", "ImagePullBackoff", "can't pull it")),
			timeoutDeploy(deploy(t, "foo", "pull-backoff"), "Timed out!"),
			image("foo", "pull-backoff"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "pull-backoff",
				WithLogURL, AllUnknownConditions,
				MarkResourcesUnavailable("ImagePullBackoff", "can't pull it"), WithContainerStatuses([]v1.ContainerStatuses{{Name: "pull-backoff"}})),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: pa("foo", "pull-backoff", WithReachability(asv1a1.ReachabilityUnreachable)),
		}},
		Key: "foo/pull-backoff",
	}, {
		Name: "surface pod errors",
		Ctx:  defaultCtx,
		// Test the propagation of the termination state of a Pod into the revision.
		// This initializes the world to the stable state after its first reconcile,
		// but changes the user deployment to have a failing pod. It then verifies
		// that Reconcile propagates this into the status of the Revision.
		Objects: []runtime.Object{
			rev("foo", "pod-error",
				withK8sServiceName("a-pod-error"), WithLogURL, AllUnknownConditions, MarkActive),
			pa("foo", "pod-error"), // PA can't be ready, since no traffic.
			pod(t, "foo", "pod-error", WithFailingContainer("pod-error", 5, "I failed man!")),
			deploy(t, "foo", "pod-error"),
			image("foo", "pod-error"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "pod-error",
				WithLogURL, AllUnknownConditions, MarkContainerExiting(5,
					v1.RevisionContainerExitingMessage("I failed man!")), WithContainerStatuses([]v1.ContainerStatuses{{Name: "pod-error"}})),
		}},
		Key: "foo/pod-error",
	}, {
		Name: "surface pod schedule errors",
		Ctx:  defaultCtx,
		// Test the propagation of the scheduling errors of Pod into the revision.
		// This initializes the world to unschedule pod. It then verifies
		// that Reconcile propagates this into the status of the Revision.
		Objects: []runtime.Object{
			rev("foo", "pod-schedule-error",
				withK8sServiceName("a-pod-schedule-error"), WithLogURL, AllUnknownConditions, MarkActive),
			pa("foo", "pod-schedule-error"), // PA can't be ready, since no traffic.
			pod(t, "foo", "pod-schedule-error", WithUnschedulableContainer("Insufficient energy", "Unschedulable")),
			deploy(t, "foo", "pod-schedule-error"),
			image("foo", "pod-schedule-error"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "pod-schedule-error",
				WithLogURL, AllUnknownConditions, MarkResourcesUnavailable("Insufficient energy",
					"Unschedulable"), WithContainerStatuses([]v1.ContainerStatuses{{Name: "pod-schedule-error"}})),
		}},
		Key: "foo/pod-schedule-error",
	}, {
		Name: "ready steady state",
		Ctx:  defaultCtx,
		// Test the transition that Reconcile makes when Endpoints become ready on the
		// SKS owned services, which is signalled by pa having service name.
		// This puts the world into the stable post-reconcile state for an Active
		// Revision.  It then creates an Endpoints resource with active subsets.
		// This signal should make our Reconcile mark the Revision as Ready.
		Objects: []runtime.Object{
			rev("foo", "steady-ready", withK8sServiceName("very-steady"), WithLogURL),
			pa("foo", "steady-ready", WithTraffic, WithPAStatusService("steadier-even")),
			deploy(t, "foo", "steady-ready"),
			image("foo", "steady-ready"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "steady-ready", withK8sServiceName("steadier-even"), WithLogURL,
				// All resources are ready to go, we should see the revision being
				// marked ready
				MarkRevisionReady, WithContainerStatuses([]v1.ContainerStatuses{{Name: "steady-ready"}})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "RevisionReady", "Revision becomes ready upon all resources being ready"),
		},
		Key: "foo/steady-ready",
	}, {
		Name:    "lost pa owner ref",
		Ctx:     defaultCtx,
		WantErr: true,
		Objects: []runtime.Object{
			rev("foo", "missing-owners", withK8sServiceName("lesser-revision"), WithLogURL,
				MarkRevisionReady),
			pa("foo", "missing-owners", WithTraffic, WithPodAutoscalerOwnersRemoved),
			deploy(t, "foo", "missing-owners"),
			image("foo", "missing-owners"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "missing-owners", withK8sServiceName("lesser-revision"), WithLogURL,
				MarkRevisionReady,
				// When we're missing the OwnerRef for PodAutoscaler we see this update.
				MarkResourceNotOwned("PodAutoscaler", "missing-owners"), WithContainerStatuses([]v1.ContainerStatuses{{Name: "missing-owners"}})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `revision: "missing-owners" does not own PodAutoscaler: "missing-owners"`),
		},
		Key: "foo/missing-owners",
	}, {
		Name:    "lost deployment owner ref",
		Ctx:     defaultCtx,
		WantErr: true,
		Objects: []runtime.Object{
			rev("foo", "missing-owners", withK8sServiceName("youre-gonna-lose"), WithLogURL,
				MarkRevisionReady, WithContainerStatuses([]v1.ContainerStatuses{{Name: "missing-owners"}})),
			pa("foo", "missing-owners", WithTraffic),
			noOwner(deploy(t, "foo", "missing-owners")),
			image("foo", "missing-owners"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "missing-owners", withK8sServiceName("youre-gonna-lose"), WithLogURL,
				MarkRevisionReady,
				// When we're missing the OwnerRef for Deployment we see this update.
				MarkResourceNotOwned("Deployment", "missing-owners-deployment"), WithContainerStatuses([]v1.ContainerStatuses{{Name: "missing-owners"}})),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `revision: "missing-owners" does not own Deployment: "missing-owners-deployment"`),
		},
		Key: "foo/missing-owners",
	}, {
		Name: "image pull secrets",
		Ctx:  defaultCtx,
		// This test case tests that the image pull secrets from revision propagate to deployment and image
		Objects: []runtime.Object{
			rev("foo", "image-pull-secrets", WithImagePullSecrets("foo-secret")),
		},
		WantCreates: []runtime.Object{
			pa("foo", "image-pull-secrets"),
			deployImagePullSecrets(deploy(t, "foo", "image-pull-secrets"), "foo-secret"),
			imagePullSecrets(image("foo", "image-pull-secrets"), "foo-secret"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "image-pull-secrets",
				WithImagePullSecrets("foo-secret"),
				WithLogURL, AllUnknownConditions, MarkDeploying("Deploying"), WithContainerStatuses([]v1.ContainerStatuses{{Name: "image-pull-secrets"}})),
		}},
		Key: "foo/image-pull-secrets",
	}, {
		Name: "initial scale unset so using cluster default init scale",
		Ctx:  autoscalerConfigCtx(false, 2),
		Objects: []runtime.Object{
			rev("foo", "initial-scale-unset"),
			pa("foo", "initial-scale-unset"),
		},
		WantCreates: []runtime.Object{
			// Replica count should be 0
			withReplicaCount(deploy(t, "foo", "initial-scale-unset"), 2),
			image("foo", "initial-scale-unset"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "initial-scale-unset",
				WithLogURL, AllUnknownConditions,
				MarkDeploying("Deploying"), WithContainerStatuses([]v1.ContainerStatuses{{Name: "initial-scale-unset"}})),
		}},
		Key: "foo/initial-scale-unset",
	}, {
		Name: "initial scale set to 0 but cluster does not allow",
		Ctx:  autoscalerConfigCtx(false, 1),
		Objects: []runtime.Object{
			rev("foo", "initial-scale-zero-not-allowed", WithInitialScale(0)),
			pa("foo", "initial-scale-zero-not-allowed"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", `Failed to update status for "initial-scale-zero-not-allowed": invalid value: 0: metadata.annotations.autoscaling.knative.dev/initialScale`),
		},
		WantErr: true,
		WantCreates: []runtime.Object{
			// Fall back to using cluster's default scale because zero is not allowed.
			withReplicaCount(initialScale(deploy(t, "foo", "initial-scale-zero-not-allowed"), 0), 1),
			initialScaleImage(image("foo", "initial-scale-zero-not-allowed"), 0),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "initial-scale-zero-not-allowed", WithInitialScale(0),
				WithLogURL, AllUnknownConditions,
				MarkDeploying("Deploying"), WithContainerStatuses([]v1.ContainerStatuses{{Name: "initial-scale-zero-not-allowed"}})),
		}},
		Key: "foo/initial-scale-zero-not-allowed",
	}, {
		Name: "initial scale set to non zero and override cluster default scale",
		Ctx:  autoscalerConfigCtx(false, 2),
		Objects: []runtime.Object{
			rev("foo", "initial-scale-not-zero-override", WithInitialScale(3)),
			pa("foo", "initial-scale-not-zero-override"),
		},
		WantCreates: []runtime.Object{
			// Replica count should be 0
			withReplicaCount(initialScale(deploy(t, "foo", "initial-scale-not-zero-override"), 3), 3),
			initialScaleImage(image("foo", "initial-scale-not-zero-override"), 3),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "initial-scale-not-zero-override", WithInitialScale(3),
				WithLogURL, AllUnknownConditions,
				MarkDeploying("Deploying"), WithContainerStatuses([]v1.ContainerStatuses{{Name: "initial-scale-not-zero-override"}})),
		}},
		Key: "foo/initial-scale-not-zero-override",
	}, {
		Name: "initial scale set to zero and cluster allows",
		Ctx:  autoscalerConfigCtx(true, 1),
		Objects: []runtime.Object{
			rev("foo", "initial-scale-zero-override", WithInitialScale(0),
				WithLogURL, AllUnknownConditions,
				MarkDeploying("Deploying"), WithContainerStatuses([]v1.ContainerStatuses{{Name: "initial-scale-zero-override"}})),
			pa("foo", "initial-scale-zero-override", WithReachability(asv1a1.ReachabilityUnknown)),
		},
		WantCreates: []runtime.Object{
			// Replica count should be 0
			withReplicaCount(initialScale(deploy(t, "foo", "initial-scale-zero-override"), 0), 0),
			initialScaleImage(image("foo", "initial-scale-zero-override"), 0),
		},
		Key: "foo/initial-scale-zero-override",
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		testConfigs := ReconcilerTestConfig()
		ctxConfig := config.FromContext(ctx)
		if ctxConfig == nil {
			testConfigs.Autoscaler, _ = autoscalerconfig.NewConfigFromMap(map[string]string{})
			testConfigs.Autoscaler.InitialScale = 1
			testConfigs.Autoscaler.AllowZeroInitialScale = false
		} else {
			testConfigs.Autoscaler = ctxConfig.Autoscaler
		}
		ctx = config.ToContext(ctx, testConfigs)
		r := &Reconciler{
			kubeclient:          kubeclient.Get(ctx),
			client:              servingclient.Get(ctx),
			cachingclient:       cachingclient.Get(ctx),
			podAutoscalerLister: listers.GetPodAutoscalerLister(),
			imageLister:         listers.GetImageLister(),
			deploymentLister:    listers.GetDeploymentLister(),
			serviceLister:       listers.GetK8sServiceLister(),
			resolver:            &nopResolver{},
		}

		return revisionreconciler.NewReconciler(ctx, logging.FromContext(ctx), servingclient.Get(ctx),
			listers.GetRevisionLister(), controller.GetEventRecorder(ctx), r,
			controller.Options{ConfigStore: &testConfigStore{config: testConfigs}})
	}))
}

func autoscalerConfigCtx(allowInitialScaleZero bool, initialScale int) context.Context {
	testConfigs := ReconcilerTestConfig()
	testConfigs.Autoscaler, _ = autoscalerconfig.NewConfigFromMap(map[string]string{})
	testConfigs.Autoscaler.AllowZeroInitialScale = allowInitialScaleZero
	testConfigs.Autoscaler.InitialScale = int32(initialScale)
	return config.ToContext(context.Background(), testConfigs)
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
		Reason:  "ProgressDeadlineExceeded",
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

func initialScale(deploy *appsv1.Deployment, initialScale int) *appsv1.Deployment {
	if deploy.ObjectMeta.Annotations == nil {
		deploy.ObjectMeta.Annotations = map[string]string{}
	}
	deploy.ObjectMeta.Annotations[autoscaling.InitialScaleAnnotationKey] = strconv.Itoa(initialScale)
	if deploy.Spec.Template.ObjectMeta.Annotations == nil {
		deploy.Spec.Template.ObjectMeta.Annotations = map[string]string{}
	}
	deploy.Spec.Template.ObjectMeta.Annotations[autoscaling.InitialScaleAnnotationKey] = strconv.Itoa(initialScale)
	return deploy
}

func withReplicaCount(deploy *appsv1.Deployment, numReplica int) *appsv1.Deployment {
	deploy.Spec.Replicas = ptr.Int32(int32(numReplica))
	return deploy
}

func initialScaleImage(image *caching.Image, initialScale int) *caching.Image {
	if image.ObjectMeta.Annotations == nil {
		image.ObjectMeta.Annotations = make(map[string]string, 1)
	}
	image.ObjectMeta.Annotations[autoscaling.InitialScaleAnnotationKey] = strconv.Itoa(initialScale)
	return image
}

func imagePullSecrets(image *caching.Image, secretName string) *caching.Image {
	image.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{
		Name: secretName,
	}}
	return image
}

func changeContainers(deploy *appsv1.Deployment) *appsv1.Deployment {
	podSpec := deploy.Spec.Template.Spec
	for i := range podSpec.Containers {
		podSpec.Containers[i].Image = "asdf"
	}
	return deploy
}

func rev(namespace, name string, ro ...RevisionOption) *v1.Revision {
	r := &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       "test-uid",
		},
		Spec: v1.RevisionSpec{
			PodSpec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  name,
					Image: "busybox",
				}},
			},
		},
	}
	r.SetDefaults(context.Background())

	for _, opt := range ro {
		opt(r)
	}
	return r
}

func withK8sServiceName(sn string) RevisionOption {
	return func(r *v1.Revision) {
		r.Status.ServiceName = sn
	}
}

// TODO(mattmoor): Come up with a better name for this.
func AllUnknownConditions(r *v1.Revision) {
	WithInitRevConditions(r)
	MarkDeploying("")(r)
	MarkActivating("Deploying", "")(r)
}

type configOption func(*config.Config)

func deploy(t *testing.T, namespace, name string, opts ...interface{}) *appsv1.Deployment {
	t.Helper()
	cfg := ReconcilerTestConfig()
	var err error
	cfg.Autoscaler, err = autoscalerconfig.NewConfigFromMap(map[string]string{})
	if err != nil {
		t.Fatalf("Error creating default autoscaler config: %v", err)
	}
	for _, opt := range opts {
		if configOpt, ok := opt.(configOption); ok {
			configOpt(cfg)
		}
	}

	rev := rev(namespace, name)

	for _, opt := range opts {
		if revOpt, ok := opt.(RevisionOption); ok {
			revOpt(rev)
		}
	}

	// Do this here instead of in `rev` itself to ensure that we populate defaults
	// before calling MakeDeployment within Reconcile.
	rev.SetDefaults(context.Background())
	deployment, err := resources.MakeDeployment(rev, cfg.Logging, cfg.Tracing, cfg.Network,
		cfg.Observability, cfg.Autoscaler, cfg.Deployment,
	)
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

	return resources.MakeImageCache(rev(namespace, name), name, "")
}

func pa(namespace, name string, ko ...PodAutoscalerOption) *asv1a1.PodAutoscaler {
	rev := rev(namespace, name)
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
		Deployment: getTestDeploymentConfig(),
		Observability: &metrics.ObservabilityConfig{
			LoggingURLTemplate: "http://logger.io/${REVISION_UID}",
		},
		Logging:    &logging.Config{},
		Tracing:    &tracingconfig.Config{},
		Autoscaler: &autoscalerconfig.Config{},
		Defaults:   &defaultconfig.Defaults{},
	}
}
