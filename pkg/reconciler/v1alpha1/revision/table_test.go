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
	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/apis/duck"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
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
	// Create short-hand aliases that pass through the above config and Active to getRev and friends.
	rev := func(namespace, name, servingState, image string) *v1alpha1.Revision {
		return getRev(namespace, name, v1alpha1.RevisionServingStateType(servingState), image)
	}
	deploy := func(namespace, name, servingState, image string) *appsv1.Deployment {
		return getDeploy(namespace, name, v1alpha1.RevisionServingStateType(servingState), image, ReconcilerTestConfig())
	}
	image := func(namespace, name, servingState, image string) *caching.Image {
		i, err := getImage(namespace, name, v1alpha1.RevisionServingStateType(servingState), image, ReconcilerTestConfig())
		if err != nil {
			t.Fatalf("Error building image: %v", err)
		}
		return i
	}
	kpa := func(namespace, name, servingState, image string) *kpav1alpha1.PodAutoscaler {
		return getKPA(namespace, name, v1alpha1.RevisionServingStateType(servingState), image)
	}
	svc := func(namespace, name, servingState, image string) *corev1.Service {
		return getService(namespace, name, v1alpha1.RevisionServingStateType(servingState), image)
	}
	endpoints := func(namespace, name, servingState, image string) *corev1.Endpoints {
		return getEndpoints(namespace, name, v1alpha1.RevisionServingStateType(servingState), image)
	}

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
			rev("foo", "first-reconcile", "Active", "busybox"),
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			kpa("foo", "first-reconcile", "Active", "busybox"),
			deploy("foo", "first-reconcile", "Active", "busybox"),
			svc("foo", "first-reconcile", "Active", "busybox"),
			image("foo", "first-reconcile", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "first-reconcile", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "first-reconcile", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
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
			rev("foo", "update-status-failure", "Active", "busybox"),
			kpa("foo", "update-status-failure", "Active", "busybox"),
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			deploy("foo", "update-status-failure", "Active", "busybox"),
			svc("foo", "update-status-failure", "Active", "busybox"),
			image("foo", "update-status-failure", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "update-status-failure", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "update-status-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
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
			rev("foo", "create-kpa-failure", "Active", "busybox"),
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			kpa("foo", "create-kpa-failure", "Active", "busybox"),
			deploy("foo", "create-kpa-failure", "Active", "busybox"),
			svc("foo", "create-kpa-failure", "Active", "busybox"),
			image("foo", "create-kpa-failure", "Active", "busybox"),
			// The user service and autoscaler resources are not created.
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "create-kpa-failure", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					LogURL:      "http://logger.io/test-uid",
					ServiceName: svc("foo", "create-kpa-failure", "Active", "busybox").Name,
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
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
			rev("foo", "create-user-deploy-failure", "Active", "busybox"),
			kpa("foo", "create-user-deploy-failure", "Active", "busybox"),
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			deploy("foo", "create-user-deploy-failure", "Active", "busybox"),
			// The user service and autoscaler resources are not created.
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "create-user-deploy-failure", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					LogURL: "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
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
			rev("foo", "create-user-service-failure", "Active", "busybox"),
			kpa("foo", "create-user-service-failure", "Active", "busybox"),
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			deploy("foo", "create-user-service-failure", "Active", "busybox"),
			svc("foo", "create-user-service-failure", "Active", "busybox"),
			image("foo", "create-user-service-failure", "Active", "busybox"),
			// No autoscaler resources created.
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "create-user-service-failure", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "create-user-service-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
		}},
		Key: "foo/create-user-service-failure",
	}, {
		Name: "stable revision reconciliation",
		// Test a simple stable reconciliation of an Active Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (immediately post-creation), and verify that no changes
		// are necessary.
		Objects: []runtime.Object{
			makeStatus(
				rev("foo", "stable-reconcile", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "stable-reconcile", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
			kpa("foo", "stable-reconcile", "Active", "busybox"),
			deploy("foo", "stable-reconcile", "Active", "busybox"),
			svc("foo", "stable-reconcile", "Active", "busybox"),
			image("foo", "stable-reconcile", "Active", "busybox"),
		},
		// No changes are made to any objects.
		Key: "foo/stable-reconcile",
	}, {
		Name: "deactivated revision is stable",
		// Test a simple stable reconciliation of a Reserve Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (port-Reserve), and verify that no changes are necessary.
		Objects: []runtime.Object{
			makeStatus(
				rev("foo", "stable-deactivation", "Reserve", "busybox"),
				// The Revision status matches that of a properly deactivated Revision.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "stable-deactivation", "Reserve", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
			kpa("foo", "stable-deactivation", "Reserve", "busybox"),
			// The Deployments match what we'd expect of an Reserve revision.
			deploy("foo", "stable-deactivation", "Reserve", "busybox"),
			svc("foo", "stable-deactivation", "Reserve", "busybox"),
			image("foo", "stable-deactivation", "Reserve", "busybox"),
		},
		Key: "foo/stable-deactivation",
	}, {
		Name: "activate a reserve revision",
		// Test the transition that's made when Active is set.
		// We initialize the world to a stable Reserve state, but make the
		// Revision's ServingState Active.  We then look for the expected
		// mutations, which should include scaling up the Deployments to
		// 1 replica each, and recreating the Kubernetes Service resources.
		Objects: []runtime.Object{
			makeStatus(
				rev("foo", "activate-revision", "Active", "busybox"),
				// The status and state of the world reflect a Reserve Revision,
				// but it has a ServingState of Active.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "activate-revision", "Reserve", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Updating",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Updating",
					}, {
						Type:    "Ready",
						Status:  "False",
						Reason:  "Inactive",
						Message: `Revision "activate-revision" is Inactive.`,
					}},
				}),
			kpa("foo", "activate-revision", "Reserve", "busybox"),
			// The Deployments match what we'd expect of an Reserve revision.
			deploy("foo", "activate-revision", "Reserve", "busybox"),
			svc("foo", "activate-revision", "Reserve", "busybox"),
			image("foo", "activate-revision", "Reserve", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "activate-revision", "Active", "busybox"),
				// After activating the Revision status looks like this.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "activate-revision", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
		}},
		Key: "foo/activate-revision",
	}, {
		Name: "create resources in reserve",
		// Test a reconcile of a Revision in the Reserve state.
		// This tests the initial set of resources that we create for a Revision
		// when it is in a Reserve state and none of its resources exist.  The main
		// place we should expect this transition to happen is Retired -> Reserve.
		Objects: []runtime.Object{
			rev("foo", "create-in-reserve", "Reserve", "busybox"),
		},
		WantCreates: []metav1.Object{
			kpa("foo", "create-in-reserve", "Reserve", "busybox"),
			// Only Deployments are created and they have no replicas.
			deploy("foo", "create-in-reserve", "Reserve", "busybox"),
			svc("foo", "create-in-reserve", "Reserve", "busybox"),
			image("foo", "create-in-reserve", "Reserve", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "create-in-reserve", "Reserve", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "create-in-reserve", "Reserve", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
		}},
		Key: "foo/create-in-reserve",
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
			makeStatus(
				rev("foo", "endpoint-created-not-ready", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "endpoint-created-not-ready", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
						// We set the LTT so that we don't give up on the Endpoints yet.
						LastTransitionTime: apis.VolatileTime{metav1.NewTime(time.Now())},
					}},
				}),
			kpa("foo", "endpoint-created-not-ready", "Active", "busybox"),
			deploy("foo", "endpoint-created-not-ready", "Active", "busybox"),
			svc("foo", "endpoint-created-not-ready", "Active", "busybox"),
			endpoints("foo", "endpoint-created-not-ready", "Active", "busybox"),
			image("foo", "endpoint-created-not-ready", "Active", "busybox"),
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
			makeStatus(
				rev("foo", "endpoint-created-timeout", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "endpoint-created-timeout", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
						// The LTT defaults and is long enough ago that we expire waiting
						// on the Endpoints to become ready.
					}},
				}),
			kpa("foo", "endpoint-created-timeout", "Active", "busybox"),
			deploy("foo", "endpoint-created-timeout", "Active", "busybox"),
			svc("foo", "endpoint-created-timeout", "Active", "busybox"),
			endpoints("foo", "endpoint-created-timeout", "Active", "busybox"),
			image("foo", "endpoint-created-timeout", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "endpoint-created-timeout", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "endpoint-created-timeout", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:    "Ready",
						Status:  "False",
						Reason:  "ServiceTimeout",
						Message: "Timed out waiting for a service endpoint to become ready",
					}, {
						Type:    "ResourcesAvailable",
						Status:  "False",
						Reason:  "ServiceTimeout",
						Message: "Timed out waiting for a service endpoint to become ready",
					}},
				}),
		}},
		// We update the Revision to timeout waiting on Endpoints.
		Key: "foo/endpoint-created-timeout",
	}, {
		Name: "endpoint and kpa are ready",
		// Test the transition that Reconcile makes when Endpoints become ready.
		// This puts the world into the stable post-reconcile state for an Active
		// Revision.  It then creates an Endpoints resource with active subsets.
		// This signal should make our Reconcile mark the Revision as Ready.
		Objects: []runtime.Object{
			makeStatus(
				rev("foo", "endpoint-ready", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "endpoint-ready", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
			addKPAStatus(
				kpa("foo", "endpoint-ready", "Active", "busybox"),
				kpav1alpha1.PodAutoscalerStatus{
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "True",
					}, {
						Type:   "Ready",
						Status: "True",
					}},
				}),
			deploy("foo", "endpoint-ready", "Active", "busybox"),
			svc("foo", "endpoint-ready", "Active", "busybox"),
			addEndpoint(endpoints("foo", "endpoint-ready", "Active", "busybox")),
			image("foo", "endpoint-ready", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "endpoint-ready", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "endpoint-ready", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "True",
					}, {
						Type:   "ContainerHealthy",
						Status: "True",
					}, {
						Type:   "Ready",
						Status: "True",
					}, {
						Type:   "ResourcesAvailable",
						Status: "True",
					}},
				}),
		}},
		// We update the Revision to timeout waiting on Endpoints.
		Key: "foo/endpoint-ready",
	}, {
		Name: "kpa not ready",
		// Test propagating the KPA status to the Revision.
		Objects: []runtime.Object{
			makeStatus(
				rev("foo", "kpa-not-ready", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "kpa-not-ready", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "True",
					}, {
						Type:   "ContainerHealthy",
						Status: "True",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
			addKPAStatus(
				kpa("foo", "kpa-not-ready", "Active", "busybox"),
				kpav1alpha1.PodAutoscalerStatus{
					Conditions: duckv1alpha1.Conditions{{
						Type:    "Active",
						Status:  "Unknown",
						Reason:  "Something",
						Message: "This is something longer",
					}, {
						Type:    "Ready",
						Status:  "Unknown",
						Reason:  "Something",
						Message: "This is something longer",
					}},
				}),
			deploy("foo", "kpa-not-ready", "Active", "busybox"),
			svc("foo", "kpa-not-ready", "Active", "busybox"),
			addEndpoint(endpoints("foo", "kpa-not-ready", "Active", "busybox")),
			image("foo", "kpa-not-ready", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "kpa-not-ready", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "kpa-not-ready", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:    "Active",
						Status:  "Unknown",
						Reason:  "Something",
						Message: "This is something longer",
					}, {
						Type:   "ContainerHealthy",
						Status: "True",
					}, {
						Type:    "Ready",
						Status:  "Unknown",
						Reason:  "Something",
						Message: "This is something longer",
					}, {
						Type:   "ResourcesAvailable",
						Status: "True",
					}},
				}),
		}},
		Key: "foo/kpa-not-ready",
	}, {
		Name: "kpa inactive",
		// Test propagating the inactivity signal from the KPA to the Revision.
		Objects: []runtime.Object{
			makeStatus(
				rev("foo", "kpa-inactive", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "kpa-inactive", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "True",
					}, {
						Type:   "ContainerHealthy",
						Status: "True",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
			addKPAStatus(
				kpa("foo", "kpa-inactive", "Active", "busybox"),
				kpav1alpha1.PodAutoscalerStatus{
					Conditions: duckv1alpha1.Conditions{{
						Type:    "Active",
						Status:  "False",
						Reason:  "NoTraffic",
						Message: "This thing is inactive.",
					}, {
						Type:    "Ready",
						Status:  "False",
						Reason:  "NoTraffic",
						Message: "This thing is inactive.",
					}},
				}),
			deploy("foo", "kpa-inactive", "Active", "busybox"),
			svc("foo", "kpa-inactive", "Active", "busybox"),
			addEndpoint(endpoints("foo", "kpa-inactive", "Active", "busybox")),
			image("foo", "kpa-inactive", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "kpa-inactive", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "kpa-inactive", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:    "Active",
						Status:  "False",
						Reason:  "NoTraffic",
						Message: "This thing is inactive.",
					}, {
						Type:   "ContainerHealthy",
						Status: "True",
					}, {
						Type:    "Ready",
						Status:  "False",
						Reason:  "NoTraffic",
						Message: "This thing is inactive.",
					}, {
						Type:   "ResourcesAvailable",
						Status: "True",
					}},
				}),
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
			makeStatus(
				rev("foo", "fix-mutated-service", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "fix-mutated-service", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
						// We set the LTT so that we don't give up on the Endpoints yet.
						LastTransitionTime: apis.VolatileTime{metav1.NewTime(time.Now())},
					}},
				}),
			kpa("foo", "fix-mutated-service", "Active", "busybox"),
			deploy("foo", "fix-mutated-service", "Active", "busybox"),
			changeService(svc("foo", "fix-mutated-service", "Active", "busybox")),
			endpoints("foo", "fix-mutated-service", "Active", "busybox"),
			image("foo", "fix-mutated-service", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// Reason changes from Deploying to Updating.
			Object: makeStatus(
				rev("foo", "fix-mutated-service", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "fix-mutated-service", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Updating",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Updating",
					}},
				}),
		}, {
			Object: svc("foo", "fix-mutated-service", "Active", "busybox"),
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
			makeStatus(
				rev("foo", "update-user-svc-failure", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "update-user-svc-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
						// We set the LTT so that we don't give up on the Endpoints yet.
						LastTransitionTime: apis.VolatileTime{metav1.NewTime(time.Now())},
					}},
				}),
			kpa("foo", "update-user-svc-failure", "Active", "busybox"),
			deploy("foo", "update-user-svc-failure", "Active", "busybox"),
			changeService(svc("foo", "update-user-svc-failure", "Active", "busybox")),
			endpoints("foo", "update-user-svc-failure", "Active", "busybox"),
			image("foo", "update-user-svc-failure", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svc("foo", "update-user-svc-failure", "Active", "busybox"),
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
			makeStatus(
				rev("foo", "deploy-timeout", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "deploy-timeout", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
						// We set the LTT so that we don't give up on the Endpoints yet.
						LastTransitionTime: apis.VolatileTime{metav1.NewTime(time.Now())},
					}},
				}),
			kpa("foo", "deploy-timeout", "Active", "busybox"),
			timeoutDeploy(deploy("foo", "deploy-timeout", "Active", "busybox")),
			svc("foo", "deploy-timeout", "Active", "busybox"),
			endpoints("foo", "deploy-timeout", "Active", "busybox"),
			image("foo", "deploy-timeout", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "deploy-timeout", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "deploy-timeout", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:    "Ready",
						Status:  "False",
						Reason:  "ProgressDeadlineExceeded",
						Message: "Unable to create pods for more than 120 seconds.",
					}, {
						Type:    "ResourcesAvailable",
						Status:  "False",
						Reason:  "ProgressDeadlineExceeded",
						Message: "Unable to create pods for more than 120 seconds.",
					}},
				}),
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
			addBuild(rev("foo", "missing-build", "Active", "busybox"), "the-build"),
		},
		WantErr: true,
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				addBuild(rev("foo", "missing-build", "Active", "busybox"), "the-build"),
				v1alpha1.RevisionStatus{
					LogURL: "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
					}, {
						Type:   "BuildSucceeded",
						Status: "Unknown",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
					}, {
						Type:   "Ready",
						Status: "Unknown",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
					}},
				}),
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
			addBuild(rev("foo", "running-build", "Active", "busybox"), "the-build"),
			build("foo", "the-build",
				duckv1alpha1.Condition{
					Type:   duckv1alpha1.ConditionSucceeded,
					Status: corev1.ConditionUnknown,
				}),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				addBuild(rev("foo", "running-build", "Active", "busybox"), "the-build"),
				v1alpha1.RevisionStatus{
					LogURL: "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
					}, {
						Type:   "BuildSucceeded",
						Status: "Unknown",
						Reason: "Building",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Building",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
					}},
				}),
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
			makeStatus(
				addBuild(rev("foo", "done-build", "Active", "busybox"), "the-build"),
				v1alpha1.RevisionStatus{
					LogURL: "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "BuildSucceeded",
						Status: "Unknown",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
					}, {
						Type:   "Ready",
						Status: "Unknown",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
					}},
				}),
			build("foo", "the-build", duckv1alpha1.Condition{
				Type:   duckv1alpha1.ConditionSucceeded,
				Status: corev1.ConditionTrue,
			}),
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			kpa("foo", "done-build", "Active", "busybox"),
			deploy("foo", "done-build", "Active", "busybox"),
			svc("foo", "done-build", "Active", "busybox"),
			image("foo", "done-build", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				addBuild(rev("foo", "done-build", "Active", "busybox"), "the-build"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "done-build", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "BuildSucceeded",
						Status: "True",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
		}},
		Key: "foo/done-build",
	}, {
		Name: "stable revision reconciliation (with build)",
		// Test a simple stable reconciliation of an Active Revision with a done Build.
		// We feed in a Revision and the resources it controls in a steady
		// state (immediately post-build completion), and verify that no changes
		// are necessary.
		Objects: []runtime.Object{
			makeStatus(
				addBuild(rev("foo", "stable-reconcile-with-build", "Active", "busybox"), "the-build"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "stable-reconcile-with-build", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "BuildSucceeded",
						Status: "True",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
			kpa("foo", "stable-reconcile-with-build", "Active", "busybox"),
			build("foo", "the-build", duckv1alpha1.Condition{
				Type:   duckv1alpha1.ConditionSucceeded,
				Status: corev1.ConditionTrue,
			}),
			deploy("foo", "stable-reconcile-with-build", "Active", "busybox"),
			svc("foo", "stable-reconcile-with-build", "Active", "busybox"),
			image("foo", "stable-reconcile-with-build", "Active", "busybox"),
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
			makeStatus(
				addBuild(rev("foo", "failed-build", "Active", "busybox"), "the-build"),
				v1alpha1.RevisionStatus{
					LogURL: "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "BuildSucceeded",
						Status: "Unknown",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
					}, {
						Type:   "Ready",
						Status: "Unknown",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
					}},
				}),
			build("foo", "the-build", duckv1alpha1.Condition{
				Type:    duckv1alpha1.ConditionSucceeded,
				Status:  corev1.ConditionFalse,
				Reason:  "SomeReason",
				Message: "This is why the build failed.",
			}),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				addBuild(rev("foo", "failed-build", "Active", "busybox"), "the-build"),
				v1alpha1.RevisionStatus{
					LogURL: "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
					}, {
						Type:    "BuildSucceeded",
						Status:  "False",
						Reason:  "SomeReason",
						Message: "This is why the build failed.",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
					}, {
						Type:    "Ready",
						Status:  "False",
						Reason:  "SomeReason",
						Message: "This is why the build failed.",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
					}},
				}),
		}},
		Key: "foo/failed-build",
	}, {
		Name: "build failed stable",
		// Test a Reconcile of a Revision with a Build that has previously failed.
		// We seed the world with a Revision that has a BuildName, and a Build that
		// has failed, which has been previously reconcile. We then verify that a
		// Reconcile has nothing to change.
		Objects: []runtime.Object{
			makeStatus(
				addBuild(rev("foo", "failed-build-stable", "Active", "busybox"), "the-build"),
				v1alpha1.RevisionStatus{
					LogURL: "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
					}, {
						Type:    "BuildSucceeded",
						Status:  "False",
						Reason:  "SomeReason",
						Message: "This is why the build failed.",
					}, {
						Type:    "Ready",
						Status:  "False",
						Reason:  "SomeReason",
						Message: "This is why the build failed.",
					}},
				}),
			build("foo", "the-build", duckv1alpha1.Condition{
				Type:    duckv1alpha1.ConditionSucceeded,
				Status:  corev1.ConditionFalse,
				Reason:  "SomeReason",
				Message: "This is why the build failed.",
			}),
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
	config := ReconcilerTestConfig()
	config.Observability.EnableVarLogCollection = true

	// Create short-hand aliases that pass through the above config and Active to getRev and friends.
	rev := func(namespace, name, servingState, image string) *v1alpha1.Revision {
		return getRev(namespace, name, v1alpha1.RevisionServingStateType(servingState), image)
	}
	deploy := func(namespace, name, servingState, image string) *appsv1.Deployment {
		return getDeploy(namespace, name, v1alpha1.RevisionServingStateType(servingState), image, config)
	}
	image := func(namespace, name, servingState, image string) *caching.Image {
		i, err := getImage(namespace, name, v1alpha1.RevisionServingStateType(servingState), image, config)
		if err != nil {
			t.Fatalf("Error building image: %v", err)
		}
		return i
	}
	kpa := func(namespace, name, servingState, image string) *kpav1alpha1.PodAutoscaler {
		return getKPA(namespace, name, v1alpha1.RevisionServingStateType(servingState), image)
	}
	svc := func(namespace, name, servingState, image string) *corev1.Service {
		return getService(namespace, name, v1alpha1.RevisionServingStateType(servingState), image)
	}

	table := TableTest{{
		Name: "first revision reconciliation (with /var/log enabled)",
		// Test a successful reconciliation flow with /var/log enabled.
		// We feed in a well formed Revision where none of its sub-resources exist,
		// and we exect it to create them and initialize the Revision's status.
		// This is similar to "first-reconcile", but should also create a fluentd configmap.
		Objects: []runtime.Object{
			rev("foo", "first-reconcile-var-log", "Active", "busybox"),
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			kpa("foo", "first-reconcile-var-log", "Active", "busybox"),
			deploy("foo", "first-reconcile-var-log", "Active", "busybox"),
			svc("foo", "first-reconcile-var-log", "Active", "busybox"),
			resources.MakeFluentdConfigMap(rev("foo", "first-reconcile-var-log", "Active", "busybox"), config.Observability),
			image("foo", "first-reconcile-var-log", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "first-reconcile-var-log", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "first-reconcile-var-log", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
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
			rev("foo", "create-configmap-failure", "Active", "busybox"),
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			deploy("foo", "create-configmap-failure", "Active", "busybox"),
			svc("foo", "create-configmap-failure", "Active", "busybox"),
			resources.MakeFluentdConfigMap(rev("foo", "create-configmap-failure", "Active", "busybox"), config.Observability),
			image("foo", "create-configmap-failure", "Active", "busybox"),
			// We don't create the autoscaler resources if we fail to create the fluentd configmap
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "create-configmap-failure", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "create-configmap-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
		}},
		Key: "foo/create-configmap-failure",
	}, {
		Name: "steady state after initial creation",
		// Verify that after creating the things from an initial reconcile that we're stable.
		Objects: []runtime.Object{
			makeStatus(
				rev("foo", "steady-state", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "steady-state", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
			kpa("foo", "steady-state", "Active", "busybox"),
			deploy("foo", "steady-state", "Active", "busybox"),
			svc("foo", "steady-state", "Active", "busybox"),
			resources.MakeFluentdConfigMap(rev("foo", "steady-state", "Active", "busybox"), config.Observability),
			image("foo", "steady-state", "Active", "busybox"),
		},
		Key: "foo/steady-state",
	}, {
		Name: "update a bad fluentd configmap",
		// Verify that after creating the things from an initial reconcile that we're stable.
		Objects: []runtime.Object{
			makeStatus(
				rev("foo", "update-fluentd-config", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "update-fluentd-config", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
			kpa("foo", "update-fluentd-config", "Active", "busybox"),
			deploy("foo", "update-fluentd-config", "Active", "busybox"),
			svc("foo", "update-fluentd-config", "Active", "busybox"),
			&corev1.ConfigMap{
				// Use the ObjectMeta, but discard the rest.
				ObjectMeta: resources.MakeFluentdConfigMap(
					rev("foo", "update-fluentd-config", "Active", "busybox"),
					config.Observability,
				).ObjectMeta,
				Data: map[string]string{
					"bad key": "bad value",
				},
			},
			image("foo", "update-fluentd-config", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// We should see a single update to the configmap we expect.
			Object: resources.MakeFluentdConfigMap(
				rev("foo", "update-fluentd-config", "Active", "busybox"),
				config.Observability,
			),
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
			makeStatus(
				rev("foo", "update-configmap-failure", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "update-configmap-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: duckv1alpha1.Conditions{{
						Type:   "Active",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Deploying",
					}},
				}),
			deploy("foo", "update-configmap-failure", "Active", "busybox"),
			svc("foo", "update-configmap-failure", "Active", "busybox"),
			&corev1.ConfigMap{
				// Use the ObjectMeta, but discard the rest.
				ObjectMeta: resources.MakeFluentdConfigMap(rev("foo", "update-configmap-failure", "Active", "busybox"), config.Observability).ObjectMeta,
				Data: map[string]string{
					"bad key": "bad value",
				},
			},
			image("foo", "update-configmap-failure", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// We should see a single update to the configmap we expect.
			Object: resources.MakeFluentdConfigMap(rev("foo", "update-configmap-failure", "Active", "busybox"), config.Observability),
		}},
		Key: "foo/update-configmap-failure",
	}}

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

var (
	boolTrue = true
)

func om(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		UID:       "test-uid",
	}
}

func makeStatus(rev *v1alpha1.Revision, status v1alpha1.RevisionStatus) *v1alpha1.Revision {
	rev.Status = status
	return rev
}

func addBuild(rev *v1alpha1.Revision, name string) *v1alpha1.Revision {
	rev.Spec.BuildName = name
	rev.Spec.BuildRef = &corev1.ObjectReference{
		APIVersion: "testing.build.knative.dev/v1alpha1",
		Kind:       "Build",
		Name:       name,
	}
	return rev
}

func addEndpoint(ep *corev1.Endpoints) *corev1.Endpoints {
	ep.Subsets = []corev1.EndpointSubset{{
		Addresses: []corev1.EndpointAddress{{IP: "127.0.0.1"}},
	}}
	return ep
}

func changeService(svc *corev1.Service) *corev1.Service {
	// An effective hammer ;-P
	svc.Spec = corev1.ServiceSpec{}
	return svc
}

func timeoutDeploy(deploy *appsv1.Deployment) *appsv1.Deployment {
	deploy.Status.Conditions = []appsv1.DeploymentCondition{{
		Type:   appsv1.DeploymentProgressing,
		Status: corev1.ConditionFalse,
		Reason: "ProgressDeadlineExceeded",
	}}
	return deploy
}

func addKPAStatus(kpa *kpav1alpha1.PodAutoscaler, status kpav1alpha1.PodAutoscalerStatus) *kpav1alpha1.PodAutoscaler {
	kpa.Status = status
	return kpa
}

// Build is a special case of resource creation because it isn't owned by
// the Revision, just tracked.
func build(namespace, name string, conds ...duckv1alpha1.Condition) *unstructured.Unstructured {
	b := &unstructured.Unstructured{}
	b.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "testing.build.knative.dev",
		Version: "v1alpha1",
		Kind:    "Build",
	})
	b.SetName(name)
	b.SetNamespace(namespace)
	b.Object["status"] = map[string]interface{}{"conditions": conds}
	u := &unstructured.Unstructured{}
	duck.FromUnstructured(b, u) // prevent panic in b.DeepCopy()
	return u
}

func getRev(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: om(namespace, name),
		Spec: v1alpha1.RevisionSpec{
			Container:    corev1.Container{Image: image},
			ServingState: servingState,
		},
	}
}

func getDeploy(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string, config *config.Config) *appsv1.Deployment {

	rev := getRev(namespace, name, servingState, image)
	return resources.MakeDeployment(rev, config.Logging, config.Network, config.Observability,
		config.Autoscaler, config.Controller)
}

func getImage(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string, config *config.Config) (*caching.Image, error) {
	rev := getRev(namespace, name, servingState, image)
	deploy := resources.MakeDeployment(rev, config.Logging, config.Network, config.Observability,
		config.Autoscaler, config.Controller)
	return resources.MakeImageCache(rev, deploy)
}

func getKPA(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string) *kpav1alpha1.PodAutoscaler {
	rev := getRev(namespace, name, servingState, image)
	return resources.MakeKPA(rev)
}

func getService(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string) *corev1.Service {
	rev := getRev(namespace, name, servingState, image)
	return resources.MakeK8sService(rev)
}

func getEndpoints(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string) *corev1.Endpoints {
	service := getService(namespace, name, servingState, image)
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: service.Namespace,
			Name:      service.Name,
		},
	}
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
