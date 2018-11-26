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
	"time"

	caching "github.com/knative/caching/pkg/apis/caching/v1alpha1"
	"github.com/knative/pkg/apis/duck"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	kpav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgotesting "k8s.io/client-go/testing"

	rtesting "github.com/knative/serving/pkg/reconciler/testing"
	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
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
		Name: "first revision reconciliation",
		// Test the simplest successful reconciliation flow.
		// We feed in a well formed Revision where none of its sub-resources exist,
		// and we exect it to create them and initialize the Revision's status.
		Objects: []runtime.Object{
			rev("foo", "first-reconcile"),
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			kpa("foo", "first-reconcile"),
			deploy("foo", "first-reconcile"),
			svc("foo", "first-reconcile"),
			image("foo", "first-reconcile"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "first-reconcile",
				// The first reconciliation Populates the following status properties.
				WithK8sServiceName, WithLogURL, AllUnknownConditions),
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
			rev("foo", "update-status-failure"),
			kpa("foo", "update-status-failure"),
		},
		WantCreates: []metav1.Object{
			// We still see the following creates before the failure is induced.
			deploy("foo", "update-status-failure"),
			svc("foo", "update-status-failure"),
			image("foo", "update-status-failure"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "update-status-failure",
				// Despite failure, the following status properties are set.
				WithK8sServiceName, WithLogURL, AllUnknownConditions),
		}},
		Key: "foo/update-status-failure",
	}, {
		Name: "failure creating kpa",
		// This starts from the first reconciliation case above and induces a failure
		// creating the kpa.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "podautoscalers"),
		},
		Objects: []runtime.Object{
			rev("foo", "create-kpa-failure"),
		},
		WantCreates: []metav1.Object{
			// We still see the following creates before the failure is induced.
			kpa("foo", "create-kpa-failure"),
			deploy("foo", "create-kpa-failure"),
			svc("foo", "create-kpa-failure"),
			image("foo", "create-kpa-failure"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "create-kpa-failure",
				// Despite failure, the following status properties are set.
				WithK8sServiceName, WithLogURL, WithInitRevConditions,
				WithNoBuild, MarkDeploying("Deploying")),
		}},
		Key: "foo/create-kpa-failure",
	}, {
		Name: "failure creating user deployment",
		// This starts from the first reconciliation case above and induces a failure
		// creating the user's deployment
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "deployments"),
		},
		Objects: []runtime.Object{
			rev("foo", "create-user-deploy-failure"),
			kpa("foo", "create-user-deploy-failure"),
		},
		WantCreates: []metav1.Object{
			// We still see the following creates before the failure is induced.
			deploy("foo", "create-user-deploy-failure"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "create-user-deploy-failure",
				// Despite failure, the following status properties are set.
				WithLogURL, WithInitRevConditions,
				WithNoBuild, MarkDeploying("Deploying")),
		}},
		Key: "foo/create-user-deploy-failure",
	}, {
		Name: "failure creating user service",
		// This starts from the first reconciliation case above and induces a failure
		// creating the user's service.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "services"),
		},
		Objects: []runtime.Object{
			rev("foo", "create-user-service-failure"),
			kpa("foo", "create-user-service-failure"),
		},
		WantCreates: []metav1.Object{
			// We still see the following creates before the failure is induced.
			deploy("foo", "create-user-service-failure"),
			svc("foo", "create-user-service-failure"),
			image("foo", "create-user-service-failure"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "create-user-service-failure",
				// Despite failure, the following status properties are set.
				WithK8sServiceName, WithLogURL, WithInitRevConditions,
				WithNoBuild, MarkDeploying("Deploying")),
		}},
		Key: "foo/create-user-service-failure",
	}, {
		Name: "stable revision reconciliation",
		// Test a simple stable reconciliation of an Active Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (immediately post-creation), and verify that no changes
		// are necessary.
		Objects: []runtime.Object{
			rev("foo", "stable-reconcile",
				WithK8sServiceName, WithLogURL, AllUnknownConditions),
			kpa("foo", "stable-reconcile"),
			deploy("foo", "stable-reconcile"),
			svc("foo", "stable-reconcile"),
			image("foo", "stable-reconcile"),
		},
		// No changes are made to any objects.
		Key: "foo/stable-reconcile",
	}, {
		Name: "deactivated revision is stable",
		// Test a simple stable reconciliation of an inactive Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (port-Reserve), and verify that no changes are necessary.
		Objects: []runtime.Object{
			rev("foo", "stable-deactivation",
				WithK8sServiceName, WithLogURL, MarkRevisionReady,
				MarkInactive("NoTraffic", "This thing is inactive.")),
			kpa("foo", "stable-deactivation",
				WithNoTraffic("NoTraffic", "This thing is inactive.")),
			deploy("foo", "stable-deactivation"),
			endpoints("foo", "stable-deactivation", WithSubsets),
			svc("foo", "stable-deactivation"),
			image("foo", "stable-deactivation"),
		},
		Key: "foo/stable-deactivation",
	}, {
		Name: "endpoint is created (not ready)",
		// Test the transition when a Revision's Endpoints are created (but not yet ready)
		// This initializes the state of the world to the steady-state after a Revision's
		// first reconciliation*.  It then introduces an Endpoints resource that's not yet
		// ready.  This should result in no change, since there isn't really any new
		// information.
		//
		// * - One caveat is that we have to explicitly set LastTransitionTime to keep us
		// from thinking we've been waiting for this Endpoint since the beginning of time
		// and declaring a timeout (this is the main difference from that test below).
		Objects: []runtime.Object{
			rev("foo", "endpoint-created-not-ready",
				WithK8sServiceName, WithLogURL, AllUnknownConditions),
			kpa("foo", "endpoint-created-not-ready"),
			deploy("foo", "endpoint-created-not-ready"),
			svc("foo", "endpoint-created-not-ready"),
			endpoints("foo", "endpoint-created-not-ready"),
			image("foo", "endpoint-created-not-ready"),
		},
		// No updates, since the endpoint didn't have meaningful status.
		Key: "foo/endpoint-created-not-ready",
	}, {
		Name: "endpoint is created (timed out)",
		// Test the transition when a Revision's Endpoints aren't ready after a long period.
		// This examines the effects of Reconcile when the Endpoints exist, but we think that
		// we've been waiting since the dawn of time because we omit LastTransitionTime from
		// our Conditions.  We should see an update to put us into a ServiceTimeout state.
		Objects: []runtime.Object{
			rev("foo", "endpoint-created-timeout",
				WithK8sServiceName, WithLogURL, AllUnknownConditions,
				MarkActive, WithEmptyLTTs),
			kpa("foo", "endpoint-created-timeout", WithTraffic),
			deploy("foo", "endpoint-created-timeout"),
			svc("foo", "endpoint-created-timeout"),
			endpoints("foo", "endpoint-created-timeout"),
			image("foo", "endpoint-created-timeout"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "endpoint-created-timeout",
				WithK8sServiceName, WithLogURL, AllUnknownConditions, MarkActive,
				// When the LTT is cleared, a reconcile will result in the
				// following mutation.
				MarkServiceTimeout),
		}},
		Key: "foo/endpoint-created-timeout",
	}, {
		Name: "endpoint and kpa are ready",
		// Test the transition that Reconcile makes when Endpoints become ready.
		// This puts the world into the stable post-reconcile state for an Active
		// Revision.  It then creates an Endpoints resource with active subsets.
		// This signal should make our Reconcile mark the Revision as Ready.
		Objects: []runtime.Object{
			rev("foo", "endpoint-ready",
				WithK8sServiceName, WithLogURL, AllUnknownConditions),
			kpa("foo", "endpoint-ready", WithTraffic),
			deploy("foo", "endpoint-ready"),
			svc("foo", "endpoint-ready"),
			endpoints("foo", "endpoint-ready", WithSubsets),
			image("foo", "endpoint-ready"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "endpoint-ready", WithK8sServiceName, WithLogURL,
				// When the endpoint and KPA are ready, then we will see the
				// Revision become ready.
				MarkRevisionReady),
		}},
		Key: "foo/endpoint-ready",
	}, {
		Name: "kpa not ready",
		// Test propagating the KPA status to the Revision.
		Objects: []runtime.Object{
			rev("foo", "kpa-not-ready",
				WithK8sServiceName, WithLogURL, MarkRevisionReady),
			kpa("foo", "kpa-not-ready",
				WithBufferedTraffic("Something", "This is something longer")),
			deploy("foo", "kpa-not-ready"),
			svc("foo", "kpa-not-ready"),
			endpoints("foo", "kpa-not-ready", WithSubsets),
			image("foo", "kpa-not-ready"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "kpa-not-ready",
				WithK8sServiceName, WithLogURL, MarkRevisionReady,
				// When we reconcile a ready state and our KPA is in an activating
				// state, we should see the following mutation.
				MarkActivating("Something", "This is something longer")),
		}},
		Key: "foo/kpa-not-ready",
	}, {
		Name: "kpa inactive",
		// Test propagating the inactivity signal from the KPA to the Revision.
		Objects: []runtime.Object{
			rev("foo", "kpa-inactive",
				WithK8sServiceName, WithLogURL, MarkRevisionReady),
			kpa("foo", "kpa-inactive",
				WithNoTraffic("NoTraffic", "This thing is inactive.")),
			deploy("foo", "kpa-inactive"),
			svc("foo", "kpa-inactive"),
			endpoints("foo", "kpa-inactive", WithSubsets),
			image("foo", "kpa-inactive"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "kpa-inactive",
				WithK8sServiceName, WithLogURL, MarkRevisionReady,
				// When we reconcile an "all ready" revision when the KPA
				// is inactive, we should see the following change.
				MarkInactive("NoTraffic", "This thing is inactive.")),
		}},
		Key: "foo/kpa-inactive",
	}, {
		Name: "mutated service gets fixed",
		// Test that we correct mutations to our K8s Service resources.
		// This initializes the world to the stable post-create reconcile, and
		// adds in mutations to the K8s services that we control.  We then
		// verify that Reconcile posts the appropriate updates to correct the
		// services back to our desired specification.
		Objects: []runtime.Object{
			rev("foo", "fix-mutated-service",
				WithK8sServiceName, WithLogURL, AllUnknownConditions),
			kpa("foo", "fix-mutated-service"),
			deploy("foo", "fix-mutated-service"),
			svc("foo", "fix-mutated-service", MutateK8sService),
			endpoints("foo", "fix-mutated-service"),
			image("foo", "fix-mutated-service"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "fix-mutated-service",
				WithK8sServiceName, WithLogURL, AllUnknownConditions,
				// When our reconciliation has to change the service
				// we should see the following mutations to status.
				MarkDeploying("Updating"), MarkActivating("Deploying", "")),
		}, {
			Object: svc("foo", "fix-mutated-service"),
		}},
		Key: "foo/fix-mutated-service",
	}, {
		Name: "failure updating user service",
		// Induce a failure updating the user service.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "services"),
		},
		Objects: []runtime.Object{
			rev("foo", "update-user-svc-failure",
				WithK8sServiceName, WithLogURL, AllUnknownConditions),
			kpa("foo", "update-user-svc-failure"),
			deploy("foo", "update-user-svc-failure"),
			svc("foo", "update-user-svc-failure", MutateK8sService),
			endpoints("foo", "update-user-svc-failure"),
			image("foo", "update-user-svc-failure"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svc("foo", "update-user-svc-failure"),
		}},
		Key: "foo/update-user-svc-failure",
	}, {
		Name: "surface deployment timeout",
		// Test the propagation of ProgressDeadlineExceeded from Deployment.
		// This initializes the world to the stable state after its first reconcile,
		// but changes the user deployment to have a ProgressDeadlineExceeded
		// condition.  It then verifies that Reconcile propagates this into the
		// status of the Revision.
		Objects: []runtime.Object{
			rev("foo", "deploy-timeout",
				WithK8sServiceName, WithLogURL, AllUnknownConditions, MarkActive),
			kpa("foo", "deploy-timeout", WithTraffic),
			timeoutDeploy(deploy("foo", "deploy-timeout")),
			svc("foo", "deploy-timeout"),
			endpoints("foo", "deploy-timeout"),
			image("foo", "deploy-timeout"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "deploy-timeout",
				WithK8sServiceName, WithLogURL, AllUnknownConditions, MarkActive,
				// When the revision is reconciled after a Deployment has
				// timed out, we should see it marked with the PDE state.
				MarkProgressDeadlineExceeded),
		}},
		Key: "foo/deploy-timeout",
	}, {
		Name: "build missing",
		// Test a Reconcile of a Revision with a Build that is not found.
		// We seed the world with a freshly created Revision that has a BuildName.
		// We then verify that a Reconcile does effectively nothing but initialize
		// the conditions of this Revision.  It is notable that unlike the tests
		// above, this will include a BuildSucceeded condition.
		Objects: []runtime.Object{
			rev("foo", "missing-build", WithBuildRef("the-build")),
		},
		WantErr: true,
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "missing-build", WithBuildRef("the-build"),
				// When we first reconcile a revision with a Build (that's missing)
				// we should see the following status changes.
				WithLogURL, WithInitRevConditions),
		}},
		Key: "foo/missing-build",
	}, {
		Name: "build running",
		// Test a Reconcile of a Revision with a Build that is not done.
		// We seed the world with a freshly created Revision that has a BuildName.
		// We then verify that a Reconcile does effectively nothing but initialize
		// the conditions of this Revision.  It is notable that unlike the tests
		// above, this will include a BuildSucceeded condition.
		Objects: []runtime.Object{
			rev("foo", "running-build", WithBuildRef("the-build")),
			build("foo", "the-build", WithSucceededUnknown("", "")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "running-build", WithBuildRef("the-build"),
				// When we first reconcile a revision with a Build (not done)
				// we should see the following status changes.
				WithLogURL, WithInitRevConditions, WithOngoingBuild),
		}},
		Key: "foo/running-build",
	}, {
		Name: "build newly done",
		// Test a Reconcile of a Revision with a Build that is just done.
		// We seed the world with a freshly created Revision that has a BuildName,
		// and a Build that has a Succeeded: True status. We then verify that a
		// Reconcile toggles the BuildSucceeded status and then acts similarly to
		// the first reconcile of a BYO-Container Revision.
		Objects: []runtime.Object{
			rev("foo", "done-build", WithBuildRef("the-build"), WithInitRevConditions),
			build("foo", "the-build", WithSucceededTrue),
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			kpa("foo", "done-build"),
			deploy("foo", "done-build"),
			svc("foo", "done-build"),
			image("foo", "done-build"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "done-build", WithBuildRef("the-build"), WithInitRevConditions,
				// When we reconcile a Revision after the Build completes, we should
				// see the following updates to its status.
				WithK8sServiceName, WithLogURL, WithSuccessfulBuild,
				MarkDeploying("Deploying"), MarkActivating("Deploying", "")),
		}},
		Key: "foo/done-build",
	}, {
		Name: "stable revision reconciliation (with build)",
		// Test a simple stable reconciliation of an Active Revision with a done Build.
		// We feed in a Revision and the resources it controls in a steady
		// state (immediately post-build completion), and verify that no changes
		// are necessary.
		Objects: []runtime.Object{
			rev("foo", "stable-reconcile-with-build",
				WithBuildRef("the-build"), WithK8sServiceName, WithLogURL,
				WithInitRevConditions, WithSuccessfulBuild,
				MarkDeploying("Deploying"), MarkActivating("Deploying", "")),
			kpa("foo", "stable-reconcile-with-build"),
			build("foo", "the-build", WithSucceededTrue),
			deploy("foo", "stable-reconcile-with-build"),
			svc("foo", "stable-reconcile-with-build"),
			image("foo", "stable-reconcile-with-build"),
		},
		// No changes are made to any objects.
		Key: "foo/stable-reconcile-with-build",
	}, {
		Name: "build newly failed",
		// Test a Reconcile of a Revision with a Build that has just failed.
		// We seed the world with a freshly created Revision that has a BuildName,
		// and a Build that has just failed. We then verify that a Reconcile toggles
		// the BuildSucceeded status and stops.
		Objects: []runtime.Object{
			rev("foo", "failed-build", WithBuildRef("the-build"), WithLogURL, WithInitRevConditions),
			build("foo", "the-build",
				WithSucceededFalse("SomeReason", "This is why the build failed.")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "failed-build",
				WithBuildRef("the-build"), WithLogURL, WithInitRevConditions,
				// When we reconcile a Revision whose build has failed, we sill see that
				// failure reflected in the Revision status as follows:
				WithFailedBuild("SomeReason", "This is why the build failed.")),
		}},
		Key: "foo/failed-build",
	}, {
		Name: "build failed stable",
		// Test a Reconcile of a Revision with a Build that has previously failed.
		// We seed the world with a Revision that has a BuildName, and a Build that
		// has failed, which has been previously reconcile. We then verify that a
		// Reconcile has nothing to change.
		Objects: []runtime.Object{
			rev("foo", "failed-build-stable", WithBuildRef("the-build"), WithInitRevConditions,
				WithLogURL, WithFailedBuild("SomeReason", "This is why the build failed.")),
			build("foo", "the-build",
				WithSucceededFalse("SomeReason", "This is why the build failed.")),
		},
		Key: "foo/failed-build-stable",
	}}

	table.Test(t, MakeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		t := &rtesting.NullTracker{}
		buildInformerFactory := KResourceTypedInformerFactory(opt)
		return &Reconciler{
			Base:             reconciler.NewBase(opt, controllerAgentName),
			revisionLister:   listers.GetRevisionLister(),
			kpaLister:        listers.GetKPALister(),
			imageLister:      listers.GetImageLister(),
			deploymentLister: listers.GetDeploymentLister(),
			serviceLister:    listers.GetK8sServiceLister(),
			endpointsLister:  listers.GetEndpointsLister(),
			configMapLister:  listers.GetConfigMapLister(),
			resolver:         &nopResolver{},
			tracker:          t,
			configStore:      &testConfigStore{config: ReconcilerTestConfig()},

			buildInformerFactory: newDuckInformerFactory(t, buildInformerFactory),
		}
	}))
}

func TestReconcileWithVarLogEnabled(t *testing.T) {
	table := TableTest{{
		Name: "first revision reconciliation (with /var/log enabled)",
		// Test a successful reconciliation flow with /var/log enabled.
		// We feed in a well formed Revision where none of its sub-resources exist,
		// and we exect it to create them and initialize the Revision's status.
		// This is similar to "first-reconcile", but should also create a fluentd configmap.
		Objects: []runtime.Object{
			rev("foo", "first-reconcile-var-log"),
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			kpa("foo", "first-reconcile-var-log"),
			deploy("foo", "first-reconcile-var-log", EnableVarLog),
			svc("foo", "first-reconcile-var-log"),
			fluentdConfigMap("foo", "first-reconcile-var-log", EnableVarLog),
			image("foo", "first-reconcile-var-log", EnableVarLog),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "first-reconcile-var-log",
				// After the first reconciliation of a Revision the status looks like this.
				WithK8sServiceName, WithLogURL, AllUnknownConditions),
		}},
		Key: "foo/first-reconcile-var-log",
	}, {
		Name: "failure creating fluentd configmap",
		// Induce a failure creating the fluentd configmap.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "configmaps"),
		},
		Objects: []runtime.Object{
			rev("foo", "create-configmap-failure"),
		},
		WantCreates: []metav1.Object{
			deploy("foo", "create-configmap-failure", EnableVarLog),
			svc("foo", "create-configmap-failure"),
			fluentdConfigMap("foo", "create-configmap-failure", EnableVarLog),
			image("foo", "create-configmap-failure", EnableVarLog),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: rev("foo", "create-configmap-failure",
				// When our first reconciliation is interrupted by a failure creating
				// the fluentd configmap, we should still see the following reflected
				// in our status.
				WithK8sServiceName, WithLogURL, WithInitRevConditions,
				WithNoBuild, MarkDeploying("Deploying")),
		}},
		Key: "foo/create-configmap-failure",
	}, {
		Name: "steady state after initial creation",
		// Verify that after creating the things from an initial reconcile that we're stable.
		Objects: []runtime.Object{
			rev("foo", "steady-state",
				WithK8sServiceName, WithLogURL, AllUnknownConditions),
			kpa("foo", "steady-state"),
			deploy("foo", "steady-state", EnableVarLog),
			svc("foo", "steady-state"),
			fluentdConfigMap("foo", "steady-state", EnableVarLog),
			image("foo", "steady-state", EnableVarLog),
		},
		Key: "foo/steady-state",
	}, {
		Name: "update a bad fluentd configmap",
		// Verify that after creating the things from an initial reconcile that we're stable.
		Objects: []runtime.Object{
			rev("foo", "update-fluentd-config",
				WithK8sServiceName, WithLogURL, AllUnknownConditions),
			kpa("foo", "update-fluentd-config"),
			deploy("foo", "update-fluentd-config", EnableVarLog),
			svc("foo", "update-fluentd-config"),
			&corev1.ConfigMap{
				// Use the ObjectMeta, but discard the rest.
				ObjectMeta: fluentdConfigMap("foo", "update-fluentd-config",
					EnableVarLog).ObjectMeta,
				Data: map[string]string{
					"bad key": "bad value",
				},
			},
			image("foo", "update-fluentd-config", EnableVarLog),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// We should see a single update to the configmap we expect.
			Object: fluentdConfigMap("foo", "update-fluentd-config", EnableVarLog),
		}},
		Key: "foo/update-fluentd-config",
	}, {
		Name: "failure updating fluentd configmap",
		// Induce a failure trying to update the fluentd configmap
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "configmaps"),
		},
		Objects: []runtime.Object{
			rev("foo", "update-configmap-failure",
				WithK8sServiceName, WithLogURL, AllUnknownConditions),
			deploy("foo", "update-configmap-failure", EnableVarLog),
			svc("foo", "update-configmap-failure"),
			&corev1.ConfigMap{
				// Use the ObjectMeta, but discard the rest.
				ObjectMeta: fluentdConfigMap("foo", "update-configmap-failure",
					EnableVarLog).ObjectMeta,
				Data: map[string]string{
					"bad key": "bad value",
				},
			},
			image("foo", "update-configmap-failure", EnableVarLog),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// We should see a single update to the configmap we expect.
			Object: fluentdConfigMap("foo", "update-configmap-failure", EnableVarLog),
		}},
		Key: "foo/update-configmap-failure",
	}}

	config := ReconcilerTestConfig()
	EnableVarLog(config)

	table.Test(t, MakeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		return &Reconciler{
			Base:             reconciler.NewBase(opt, controllerAgentName),
			revisionLister:   listers.GetRevisionLister(),
			kpaLister:        listers.GetKPALister(),
			imageLister:      listers.GetImageLister(),
			deploymentLister: listers.GetDeploymentLister(),
			serviceLister:    listers.GetK8sServiceLister(),
			endpointsLister:  listers.GetEndpointsLister(),
			configMapLister:  listers.GetConfigMapLister(),
			resolver:         &nopResolver{},
			tracker:          &rtesting.NullTracker{},
			configStore:      &testConfigStore{config: config},
		}
	}))
}

func timeoutDeploy(deploy *appsv1.Deployment) *appsv1.Deployment {
	deploy.Status.Conditions = []appsv1.DeploymentCondition{{
		Type:   appsv1.DeploymentProgressing,
		Status: corev1.ConditionFalse,
		Reason: "ProgressDeadlineExceeded",
	}}
	return deploy
}

// Build is a special case of resource creation because it isn't owned by
// the Revision, just tracked.
func build(namespace, name string, bo ...BuildOption) *unstructured.Unstructured {
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "testing.build.knative.dev",
		Version: "v1alpha1",
		Kind:    "Build",
	})
	b.SetName(name)
	b.SetNamespace(namespace)
	u := &unstructured.Unstructured{}
	duck.FromUnstructured(b, u) // prevent panic in b.DeepCopy()

	for _, opt := range bo {
		opt(u)
	}
	return u
}

func rev(namespace, name string, ro ...RevisionOption) *v1alpha1.Revision {
	r := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       "test-uid",
		},
		Spec: v1alpha1.RevisionSpec{
			Container:      corev1.Container{Image: "busybox"},
			TimeoutSeconds: &metav1.Duration{Duration: 60 * time.Second},
		},
	}

	for _, opt := range ro {
		opt(r)
	}
	return r
}

func WithK8sServiceName(r *v1alpha1.Revision) {
	r.Status.ServiceName = svc(r.Namespace, r.Name).Name
}

// TODO(mattmoor): Come up with a better name for this.
func AllUnknownConditions(r *v1alpha1.Revision) {
	WithInitRevConditions(r)
	WithNoBuild(r)
	MarkDeploying("Deploying")(r)
	MarkActivating("Deploying", "")(r)
}

type configOption func(*config.Config)

func deploy(namespace, name string, co ...configOption) *appsv1.Deployment {
	config := ReconcilerTestConfig()
	for _, opt := range co {
		opt(config)
	}

	rev := rev(namespace, name)
	return resources.MakeDeployment(rev, config.Logging, config.Network, config.Observability,
		config.Autoscaler, config.Controller)
}

func image(namespace, name string, co ...configOption) *caching.Image {
	config := ReconcilerTestConfig()
	for _, opt := range co {
		opt(config)
	}

	rev := rev(namespace, name)
	deploy := resources.MakeDeployment(rev, config.Logging, config.Network, config.Observability,
		config.Autoscaler, config.Controller)
	img, err := resources.MakeImageCache(rev, deploy)
	if err != nil {
		panic(err.Error())
	}
	return img
}

func fluentdConfigMap(namespace, name string, co ...configOption) *corev1.ConfigMap {
	config := ReconcilerTestConfig()
	for _, opt := range co {
		opt(config)
	}

	rev := rev(namespace, name)
	return resources.MakeFluentdConfigMap(rev, config.Observability)
}

func kpa(namespace, name string, ko ...KPAOption) *kpav1alpha1.PodAutoscaler {
	rev := rev(namespace, name)
	k := resources.MakeKPA(rev)

	for _, opt := range ko {
		opt(k)
	}
	return k
}

func svc(namespace, name string, so ...K8sServiceOption) *corev1.Service {
	rev := rev(namespace, name)
	s := resources.MakeK8sService(rev)
	for _, opt := range so {
		opt(s)
	}
	return s
}

func endpoints(namespace, name string, eo ...EndpointsOption) *corev1.Endpoints {
	service := svc(namespace, name)
	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: service.Namespace,
			Name:      service.Name,
		},
	}
	for _, opt := range eo {
		opt(ep)
	}
	return ep
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

func (t *testConfigStore) Load() *config.Config {
	return t.config
}

func (t *testConfigStore) WatchConfigs(w configmap.Watcher) {}

var _ configStore = (*testConfigStore)(nil)

func ReconcilerTestConfig() *config.Config {
	return &config.Config{
		Controller: getTestControllerConfig(),
		Network:    &config.Network{IstioOutboundIPRanges: "*"},
		Observability: &config.Observability{
			LoggingURLTemplate: "http://logger.io/${REVISION_UID}",
		},
		Logging:    &logging.Config{},
		Autoscaler: &autoscaler.Config{},
	}
}

func EnableVarLog(cfg *config.Config) {
	cfg.Observability.EnableVarLogCollection = true
}
