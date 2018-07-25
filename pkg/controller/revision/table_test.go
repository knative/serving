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
	"testing"
	"time"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/revision/config"
	"github.com/knative/serving/pkg/controller/revision/resources"
	"github.com/knative/serving/pkg/logging"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	. "github.com/knative/serving/pkg/controller/testing"
)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	networkConfig := &config.Network{IstioOutboundIPRanges: "*"}
	loggingConfig := &logging.Config{}
	observabilityConfig := &config.Observability{
		LoggingURLTemplate: "http://logger.io/${REVISION_UID}",
	}
	autoscalerConfig := &autoscaler.Config{}
	controllerConfig := getTestControllerConfig()

	// Create short-hand aliases that pass through the above config and Active to getRev and friends.
	rev := func(namespace, name, servingState, image string) *v1alpha1.Revision {
		return getRev(namespace, name, v1alpha1.RevisionServingStateType(servingState), image,
			loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
	}
	deploy := func(namespace, name, servingState, image string) *appsv1.Deployment {
		return getDeploy(namespace, name, v1alpha1.RevisionServingStateType(servingState), image,
			loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
	}
	svc := func(namespace, name, servingState, image string) *corev1.Service {
		return getService(namespace, name, v1alpha1.RevisionServingStateType(servingState), image,
			loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
	}
	endpoints := func(namespace, name, servingState, image string) *corev1.Endpoints {
		return getEndpoints(namespace, name, v1alpha1.RevisionServingStateType(servingState), image,
			loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
	}
	deployAS := func(namespace, name, servingState, image string) *appsv1.Deployment {
		return getASDeploy(namespace, name, v1alpha1.RevisionServingStateType(servingState), image,
			loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
	}
	svcAS := func(namespace, name, servingState, image string) *corev1.Service {
		return getASService(namespace, name, v1alpha1.RevisionServingStateType(servingState), image,
			loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
	}
	endpointsAS := func(namespace, name, servingState, image string) *corev1.Endpoints {
		return getASEndpoints(namespace, name, v1alpha1.RevisionServingStateType(servingState), image,
			loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
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
			deploy("foo", "first-reconcile", "Active", "busybox"),
			svc("foo", "first-reconcile", "Active", "busybox"),
			deployAS("foo", "first-reconcile", "Active", "busybox"),
			svcAS("foo", "first-reconcile", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "first-reconcile", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "first-reconcile", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			deploy("foo", "update-status-failure", "Active", "busybox"),
			svc("foo", "update-status-failure", "Active", "busybox"),
			deployAS("foo", "update-status-failure", "Active", "busybox"),
			svcAS("foo", "update-status-failure", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "update-status-failure", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "update-status-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
		}},
		Key: "foo/update-status-failure",
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
					Conditions: []v1alpha1.RevisionCondition{{
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
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			deploy("foo", "create-user-service-failure", "Active", "busybox"),
			svc("foo", "create-user-service-failure", "Active", "busybox"),
			// No autoscaler resources created.
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "create-user-service-failure", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "create-user-service-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
		}},
		Key: "foo/create-user-service-failure",
	}, {
		Name: "failure creating autoscaler deployment",
		// This starts from the first reconciliation case above and induces a failure
		// creating the autoscaler's deployment
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "deployments"),
		},
		Objects: []runtime.Object{
			rev("foo", "create-as-deploy-failure", "Active", "busybox"),
			deploy("foo", "create-as-deploy-failure", "Active", "busybox"),
			svc("foo", "create-as-deploy-failure", "Active", "busybox"),
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			deployAS("foo", "create-as-deploy-failure", "Active", "busybox"),
			// The user service and autoscaler resources are not created.
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "create-as-deploy-failure", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					LogURL:      "http://logger.io/test-uid",
					ServiceName: svc("foo", "create-as-deploy-failure", "Active", "busybox").Name,
					Conditions: []v1alpha1.RevisionCondition{{
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
		}},
		Key: "foo/create-as-deploy-failure",
	}, {
		Name: "failure creating autoscaler service",
		// This starts from the first reconciliation case above and induces a failure
		// creating the autoscaler's service
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "services"),
		},
		Objects: []runtime.Object{
			rev("foo", "create-as-svc-failure", "Active", "busybox"),
			deploy("foo", "create-as-svc-failure", "Active", "busybox"),
			deployAS("foo", "create-as-svc-failure", "Active", "busybox"),
			svc("foo", "create-as-svc-failure", "Active", "busybox"),
		},
		WantCreates: []metav1.Object{
			svcAS("foo", "create-as-svc-failure", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "create-as-svc-failure", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					LogURL:      "http://logger.io/test-uid",
					ServiceName: svc("foo", "create-as-svc-failure", "Active", "busybox").Name,
					Conditions: []v1alpha1.RevisionCondition{{
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
		}},
		Key: "foo/create-as-svc-failure",
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
					Conditions: []v1alpha1.RevisionCondition{{
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
			deploy("foo", "stable-reconcile", "Active", "busybox"),
			deployAS("foo", "stable-reconcile", "Active", "busybox"),
			svc("foo", "stable-reconcile", "Active", "busybox"),
			svcAS("foo", "stable-reconcile", "Active", "busybox"),
		},
		// No changes are made to any objects.
		Key: "foo/stable-reconcile",
	}, {
		Name: "deactivate a revision",
		// Test the transition that's made when Reserve is set.
		// We initialize the world to a stable Active state, but make the
		// Revision's ServingState Reserve.  We then looks for the expected
		// mutations, which should include reducing the Deployments to 0 replicas
		// and deleting the Kubernetes Service resources.
		Objects: []runtime.Object{
			makeStatus(
				// The revision has been set to Deactivated, but all of the objects
				// reflect being Active.
				rev("foo", "deactivate", "Reserve", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "deactivate", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
			// The Deployments match what we'd expect of an Active revision.
			deploy("foo", "deactivate", "Active", "busybox"),
			deployAS("foo", "deactivate", "Active", "busybox"),
			// The Services match what we'd expect of an Active revision.
			svc("foo", "deactivate", "Active", "busybox"),
			svcAS("foo", "deactivate", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "deactivate", "Reserve", "busybox"),
				// After reconciliation, the status will change to reflect that this is being Deactivated.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "deactivate", "Reserve", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
						Message: `Revision "deactivate" is Inactive.`,
					}},
				}),
		}, {
			Object: deploy("foo", "deactivate", "Reserve", "busybox"),
		}, {
			Object: deployAS("foo", "deactivate", "Reserve", "busybox"),
		}},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			Name: svc("foo", "deactivate", "Reserve", "busybox").Name,
		}, {
			Name: svcAS("foo", "deactivate", "Reserve", "busybox").Name,
		}},
		// We update the Deployments to have zero replicas and delete the K8s Services when we deactivate.
		Key: "foo/deactivate",
	}, {
		Name: "failure updating user deployment",
		// Induce a failure updating the user deployment
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "deployments"),
		},
		Objects: []runtime.Object{
			makeStatus(
				// The revision has been set to Deactivated, but all of the objects
				// reflect being Active.
				rev("foo", "update-user-deploy-failure", "Reserve", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "update-user-deploy-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
			// The Deployments match what we'd expect of an Active revision.
			deploy("foo", "update-user-deploy-failure", "Active", "busybox"),
			deployAS("foo", "update-user-deploy-failure", "Active", "busybox"),
			// The Services match what we'd expect of an Active revision.
			svc("foo", "update-user-deploy-failure", "Active", "busybox"),
			svcAS("foo", "update-user-deploy-failure", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: deploy("foo", "update-user-deploy-failure", "Reserve", "busybox"),
			// We don't get to updating the autoscaler deployment or deleting services.
		}},
		// We update the Deployments to have zero replicas and delete the K8s Services when we deactivate.
		Key: "foo/update-user-deploy-failure",
	}, {
		Name: "failure updating autoscaler deployment",
		// Induce a failure updating the autoscaler deployment
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "deployments"),
		},
		Objects: []runtime.Object{
			makeStatus(
				// The revision has been set to Deactivated, but all of the objects
				// reflect being Active.
				rev("foo", "update-user-deploy-failure", "Reserve", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "update-user-deploy-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
			// The Deployments match what we'd expect of an Active revision.
			deploy("foo", "update-user-deploy-failure", "Reserve", "busybox"),
			deployAS("foo", "update-user-deploy-failure", "Active", "busybox"),
			// The Services match what we'd expect of an Active revision.
			svc("foo", "update-user-deploy-failure", "Active", "busybox"),
			svcAS("foo", "update-user-deploy-failure", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "update-user-deploy-failure", "Reserve", "busybox"),
				// After reconciliation, the status will change to reflect that this is being Deactivated.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "update-user-deploy-failure", "Reserve", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:    "Ready",
						Status:  "False",
						Reason:  "Inactive",
						Message: `Revision "update-user-deploy-failure" is Inactive.`,
					}},
				}),
		}, {
			Object: deployAS("foo", "update-user-deploy-failure", "Reserve", "busybox"),
		}},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			Name: svc("foo", "update-user-deploy-failure", "Reserve", "busybox").Name,
			// We don't reach deleting the autoscaler service.
		}},
		// We update the Deployments to have zero replicas and delete the K8s Services when we deactivate.
		Key: "foo/update-user-deploy-failure",
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
					Conditions: []v1alpha1.RevisionCondition{{
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
						Message: `Revision "stable-deactivation" is Inactive.`,
					}},
				}),
			// The Deployments match what we'd expect of an Reserve revision.
			deploy("foo", "stable-deactivation", "Reserve", "busybox"),
			deployAS("foo", "stable-deactivation", "Reserve", "busybox"),
		},
		Key: "foo/stable-deactivation",
	}, {
		Name: "retire a revision",
		// Test the transition that's made when Retired is set.
		// We initialize the world to a stable Active state, but make the
		// Revision's ServingState Retired.  We then looks for the expected
		// mutations, which should include the deletion of all Kubernetes
		// resources.
		Objects: []runtime.Object{
			makeStatus(
				// The revision has been set to Retired, but all of the objects
				// reflect being Active.
				rev("foo", "retire", "Retired", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "retire", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
			// The Deployments match what we'd expect of an Active revision.
			deploy("foo", "retire", "Active", "busybox"),
			deployAS("foo", "retire", "Active", "busybox"),
			// The Services match what we'd expect of an Active revision.
			svc("foo", "retire", "Active", "busybox"),
			svcAS("foo", "retire", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "retire", "Retired", "busybox"),
				// After reconciliation, the status will change to reflect that this is being Retired.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "retire", "Retired", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:    "Ready",
						Status:  "False",
						Reason:  "Inactive",
						Message: `Revision "retire" is Inactive.`,
					}},
				}),
		}},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			Name: deploy("foo", "retire", "Retired", "busybox").Name,
		}, {
			Name: svc("foo", "retire", "Retired", "busybox").Name,
		}, {
			Name: deployAS("foo", "retire", "Retired", "busybox").Name,
		}, {
			Name: svcAS("foo", "retire", "Retired", "busybox").Name,
		}},
		// We delete a bunch of stuff when we retire.
		Key: "foo/retire",
	}, {
		Name: "failure deleting user deployment",
		// Induce a failure deleting the user's deployment
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("delete", "deployments"),
		},
		Objects: []runtime.Object{
			makeStatus(
				// The revision has been set to Retired, but all of the objects
				// reflect being Active.
				rev("foo", "delete-user-deploy-failure", "Retired", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "delete-user-deploy-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
			// The Deployments match what we'd expect of an Active revision.
			deploy("foo", "delete-user-deploy-failure", "Active", "busybox"),
			deployAS("foo", "delete-user-deploy-failure", "Active", "busybox"),
			// The Services match what we'd expect of an Active revision.
			svc("foo", "delete-user-deploy-failure", "Active", "busybox"),
			svcAS("foo", "delete-user-deploy-failure", "Active", "busybox"),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			Name: deploy("foo", "delete-user-deploy-failure", "Retired", "busybox").Name,
			// We don't get to deleting anything else.
		}},
		// We delete a bunch of stuff when we retire.
		Key: "foo/delete-user-deploy-failure",
	}, {
		Name: "failure deleting user service",
		// Induce a failure deleting the user's service
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("delete", "services"),
		},
		Objects: []runtime.Object{
			makeStatus(
				// The revision has been set to Retired, but all of the objects
				// reflect being Active.
				rev("foo", "delete-user-svc-failure", "Retired", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "delete-user-svc-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
			// The Deployments match what we'd expect of an Active revision.
			deployAS("foo", "delete-user-svc-failure", "Active", "busybox"),
			// The Services match what we'd expect of an Active revision.
			svc("foo", "delete-user-svc-failure", "Active", "busybox"),
			svcAS("foo", "delete-user-svc-failure", "Active", "busybox"),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			Name: svc("foo", "delete-user-svc-failure", "Active", "busybox").Name,
			// We don't get to deleting anything else.
		}},
		// We delete a bunch of stuff when we retire.
		Key: "foo/delete-user-svc-failure",
	}, {
		Name: "failure deleting autoscaler deployment",
		// Induce a failure deleting the autoscaler deployment
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("delete", "deployments"),
		},
		Objects: []runtime.Object{
			makeStatus(
				// The revision has been set to Retired, but all of the objects
				// reflect being Active.
				rev("foo", "delete-as-deploy-failure", "Retired", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "delete-as-deploy-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
			// The Deployments match what we'd expect of an Active revision.
			deployAS("foo", "delete-as-deploy-failure", "Active", "busybox"),
			// The Services match what we'd expect of an Active revision.
			svcAS("foo", "delete-as-deploy-failure", "Active", "busybox"),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			Name: deployAS("foo", "delete-as-deploy-failure", "Active", "busybox").Name,
			// We don't get to deleting anything else.
		}},
		// We delete a bunch of stuff when we retire.
		Key: "foo/delete-as-deploy-failure",
	}, {
		Name: "failure deleting autoscaler service",
		// Induce a failure deleting the autoscaler service
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("delete", "services"),
		},
		Objects: []runtime.Object{
			makeStatus(
				// The revision has been set to Retired, but all of the objects
				// reflect being Active.
				rev("foo", "delete-as-svc-failure", "Retired", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "delete-as-svc-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
			// The Services match what we'd expect of an Active revision.
			svcAS("foo", "delete-as-svc-failure", "Active", "busybox"),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			Name: deployAS("foo", "delete-as-svc-failure", "Active", "busybox").Name,
			// We don't get to deleting anything else.
		}},
		// We delete a bunch of stuff when we retire.
		Key: "foo/delete-as-svc-failure",
	}, {
		Name: "retired revision is stable",
		// Test a simple stable reconciliation of a Retired Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (port-Retired), and verify that no changes are necessary.
		Objects: []runtime.Object{
			makeStatus(
				rev("foo", "stable-retirement", "Retired", "busybox"),
				// The Status properly reflects that of a Retired revision.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "stable-retirement", "Retired", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:    "Ready",
						Status:  "False",
						Reason:  "Inactive",
						Message: `Revision "stable-retirement" is Inactive.`,
					}},
				}),
		},
		Key: "foo/stable-retirement",
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
					Conditions: []v1alpha1.RevisionCondition{{
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
			// The Deployments match what we'd expect of an Reserve revision.
			deploy("foo", "activate-revision", "Reserve", "busybox"),
			deployAS("foo", "activate-revision", "Reserve", "busybox"),
		},
		WantCreates: []metav1.Object{
			// Activation should recreate the K8s Services
			svc("foo", "activate-revision", "Active", "busybox"),
			svcAS("foo", "activate-revision", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "activate-revision", "Active", "busybox"),
				// After activating the Revision status looks like this.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "activate-revision", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
		}, {
			Object: deploy("foo", "activate-revision", "Active", "busybox"),
		}, {
			Object: deployAS("foo", "activate-revision", "Active", "busybox"),
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
			// Only Deployments are created and they have no replicas.
			deploy("foo", "create-in-reserve", "Reserve", "busybox"),
			deployAS("foo", "create-in-reserve", "Reserve", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "create-in-reserve", "Reserve", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "create-in-reserve", "Reserve", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
					Conditions: []v1alpha1.RevisionCondition{{
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
						LastTransitionTime: metav1.NewTime(time.Now()),
					}},
				}),
			deploy("foo", "endpoint-created-not-ready", "Active", "busybox"),
			deployAS("foo", "endpoint-created-not-ready", "Active", "busybox"),
			svc("foo", "endpoint-created-not-ready", "Active", "busybox"),
			svcAS("foo", "endpoint-created-not-ready", "Active", "busybox"),
			endpoints("foo", "endpoint-created-not-ready", "Active", "busybox"),
			endpointsAS("foo", "endpoint-created-not-ready", "Active", "busybox"),
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
					Conditions: []v1alpha1.RevisionCondition{{
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
			deploy("foo", "endpoint-created-timeout", "Active", "busybox"),
			deployAS("foo", "endpoint-created-timeout", "Active", "busybox"),
			svc("foo", "endpoint-created-timeout", "Active", "busybox"),
			svcAS("foo", "endpoint-created-timeout", "Active", "busybox"),
			endpoints("foo", "endpoint-created-timeout", "Active", "busybox"),
			endpointsAS("foo", "endpoint-created-timeout", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "endpoint-created-timeout", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "endpoint-created-timeout", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:    "ResourcesAvailable",
						Status:  "False",
						Reason:  "ServiceTimeout",
						Message: "Timed out waiting for a service endpoint to become ready",
					}, {
						Type:    "Ready",
						Status:  "False",
						Reason:  "ServiceTimeout",
						Message: "Timed out waiting for a service endpoint to become ready",
					}},
				}),
		}},
		// We update the Revision to timeout waiting on Endpoints.
		Key: "foo/endpoint-created-timeout",
	}, {
		Name: "endpoint is ready",
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
					Conditions: []v1alpha1.RevisionCondition{{
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
			deploy("foo", "endpoint-ready", "Active", "busybox"),
			deployAS("foo", "endpoint-ready", "Active", "busybox"),
			svc("foo", "endpoint-ready", "Active", "busybox"),
			svcAS("foo", "endpoint-ready", "Active", "busybox"),
			addEndpoint(endpoints("foo", "endpoint-ready", "Active", "busybox")),
			addEndpoint(endpointsAS("foo", "endpoint-ready", "Active", "busybox")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "endpoint-ready", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "endpoint-ready", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   "ResourcesAvailable",
						Status: "True",
					}, {
						Type:   "ContainerHealthy",
						Status: "True",
					}, {
						Type:   "Ready",
						Status: "True",
					}},
				}),
		}},
		// We update the Revision to timeout waiting on Endpoints.
		Key: "foo/endpoint-ready",
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
					Conditions: []v1alpha1.RevisionCondition{{
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
						LastTransitionTime: metav1.NewTime(time.Now()),
					}},
				}),
			deploy("foo", "fix-mutated-service", "Active", "busybox"),
			deployAS("foo", "fix-mutated-service", "Active", "busybox"),
			changeService(svc("foo", "fix-mutated-service", "Active", "busybox")),
			changeService(svcAS("foo", "fix-mutated-service", "Active", "busybox")),
			endpoints("foo", "fix-mutated-service", "Active", "busybox"),
			endpointsAS("foo", "fix-mutated-service", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// Reason changes from Deploying to Updating.
			Object: makeStatus(
				rev("foo", "fix-mutated-service", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "fix-mutated-service", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   "ResourcesAvailable",
						Status: "Unknown",
						Reason: "Updating",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Updating",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Updating",
					}},
				}),
		}, {
			Object: svc("foo", "fix-mutated-service", "Active", "busybox"),
		}, {
			Object: svcAS("foo", "fix-mutated-service", "Active", "busybox"),
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
					Conditions: []v1alpha1.RevisionCondition{{
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
						LastTransitionTime: metav1.NewTime(time.Now()),
					}},
				}),
			deploy("foo", "update-user-svc-failure", "Active", "busybox"),
			deployAS("foo", "update-user-svc-failure", "Active", "busybox"),
			changeService(svc("foo", "update-user-svc-failure", "Active", "busybox")),
			svcAS("foo", "update-user-svc-failure", "Active", "busybox"),
			endpoints("foo", "update-user-svc-failure", "Active", "busybox"),
			endpointsAS("foo", "update-user-svc-failure", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svc("foo", "update-user-svc-failure", "Active", "busybox"),
		}},
		Key: "foo/update-user-svc-failure",
	}, {
		Name: "failure updating autoscaler service",
		// Induce a failure updating the autoscaler service.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "services"),
		},
		Objects: []runtime.Object{
			makeStatus(
				rev("foo", "update-as-svc-failure", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "update-as-svc-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
						LastTransitionTime: metav1.NewTime(time.Now()),
					}},
				}),
			deploy("foo", "update-as-svc-failure", "Active", "busybox"),
			deployAS("foo", "update-as-svc-failure", "Active", "busybox"),
			svc("foo", "update-as-svc-failure", "Active", "busybox"),
			changeService(svcAS("foo", "update-as-svc-failure", "Active", "busybox")),
			endpoints("foo", "update-as-svc-failure", "Active", "busybox"),
			endpointsAS("foo", "update-as-svc-failure", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcAS("foo", "update-as-svc-failure", "Active", "busybox"),
		}},
		Key: "foo/update-as-svc-failure",
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
					Conditions: []v1alpha1.RevisionCondition{{
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
						LastTransitionTime: metav1.NewTime(time.Now()),
					}},
				}),
			timeoutDeploy(deploy("foo", "deploy-timeout", "Active", "busybox")),
			deployAS("foo", "deploy-timeout", "Active", "busybox"),
			svc("foo", "deploy-timeout", "Active", "busybox"),
			svcAS("foo", "deploy-timeout", "Active", "busybox"),
			endpoints("foo", "deploy-timeout", "Active", "busybox"),
			endpointsAS("foo", "deploy-timeout", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "deploy-timeout", "Active", "busybox"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "deploy-timeout", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   "ContainerHealthy",
						Status: "Unknown",
						Reason: "Deploying",
					}, {
						Type:    "ResourcesAvailable",
						Status:  "False",
						Reason:  "ProgressDeadlineExceeded",
						Message: "Unable to create pods for more than 120 seconds.",
					}, {
						Type:    "Ready",
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
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   "ResourcesAvailable",
						Status: "Unknown",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
					}, {
						Type:   "Ready",
						Status: "Unknown",
					}, {
						Type:   "BuildSucceeded",
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
				buildv1alpha1.BuildCondition{
					Type:   buildv1alpha1.BuildSucceeded,
					Status: corev1.ConditionUnknown,
				}),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				addBuild(rev("foo", "running-build", "Active", "busybox"), "the-build"),
				v1alpha1.RevisionStatus{
					LogURL: "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   "ResourcesAvailable",
						Status: "Unknown",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
					}, {
						Type:   "BuildSucceeded",
						Status: "Unknown",
						Reason: "Building",
					}, {
						Type:   "Ready",
						Status: "Unknown",
						Reason: "Building",
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
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   "ResourcesAvailable",
						Status: "Unknown",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
					}, {
						Type:   "Ready",
						Status: "Unknown",
					}, {
						Type:   "BuildSucceeded",
						Status: "Unknown",
					}},
				}),
			build("foo", "the-build", buildv1alpha1.BuildCondition{
				Type:   buildv1alpha1.BuildSucceeded,
				Status: corev1.ConditionTrue,
			}),
		},
		WantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			deploy("foo", "done-build", "Active", "busybox"),
			svc("foo", "done-build", "Active", "busybox"),
			deployAS("foo", "done-build", "Active", "busybox"),
			svcAS("foo", "done-build", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				addBuild(rev("foo", "done-build", "Active", "busybox"), "the-build"),
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "done-build", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   "BuildSucceeded",
						Status: "True",
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
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   "BuildSucceeded",
						Status: "True",
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
			build("foo", "the-build", buildv1alpha1.BuildCondition{
				Type:   buildv1alpha1.BuildSucceeded,
				Status: corev1.ConditionTrue,
			}),
			deploy("foo", "stable-reconcile-with-build", "Active", "busybox"),
			deployAS("foo", "stable-reconcile-with-build", "Active", "busybox"),
			svc("foo", "stable-reconcile-with-build", "Active", "busybox"),
			svcAS("foo", "stable-reconcile-with-build", "Active", "busybox"),
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
					Conditions: []v1alpha1.RevisionCondition{{
						Type:   "ResourcesAvailable",
						Status: "Unknown",
					}, {
						Type:   "ContainerHealthy",
						Status: "Unknown",
					}, {
						Type:   "Ready",
						Status: "Unknown",
					}, {
						Type:   "BuildSucceeded",
						Status: "Unknown",
					}},
				}),
			build("foo", "the-build", buildv1alpha1.BuildCondition{
				Type:    buildv1alpha1.BuildSucceeded,
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
					Conditions: []v1alpha1.RevisionCondition{{
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
					Conditions: []v1alpha1.RevisionCondition{{
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
			build("foo", "the-build", buildv1alpha1.BuildCondition{
				Type:    buildv1alpha1.BuildSucceeded,
				Status:  corev1.ConditionFalse,
				Reason:  "SomeReason",
				Message: "This is why the build failed.",
			}),
		},
		Key: "foo/failed-build-stable",
	}}

	table.Test(t, func(listers *Listers, opt controller.Options) controller.Interface {
		return &Controller{
			Base:                controller.NewBase(opt, controllerAgentName, "Revisions"),
			revisionLister:      listers.GetRevisionLister(),
			buildLister:         listers.GetBuildLister(),
			deploymentLister:    listers.GetDeploymentLister(),
			serviceLister:       listers.GetK8sServiceLister(),
			endpointsLister:     listers.GetEndpointsLister(),
			configMapLister:     listers.GetConfigMapLister(),
			controllerConfig:    controllerConfig,
			networkConfig:       networkConfig,
			loggingConfig:       loggingConfig,
			observabilityConfig: observabilityConfig,
			autoscalerConfig:    autoscalerConfig,
			resolver:            &nopResolver{},
			buildtracker:        &buildTracker{builds: map[key]set{}},
		}
	})
}

func TestReconcileWithVarLogEnabled(t *testing.T) {
	networkConfig := &config.Network{IstioOutboundIPRanges: "*"}
	loggingConfig := &logging.Config{}
	observabilityConfig := &config.Observability{
		LoggingURLTemplate:     "http://logger.io/${REVISION_UID}",
		EnableVarLogCollection: true,
	}
	autoscalerConfig := &autoscaler.Config{}
	controllerConfig := getTestControllerConfig()

	// Create short-hand aliases that pass through the above config and Active to getRev and friends.
	rev := func(namespace, name, servingState, image string) *v1alpha1.Revision {
		return getRev(namespace, name, v1alpha1.RevisionServingStateType(servingState), image,
			loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
	}
	deploy := func(namespace, name, servingState, image string) *appsv1.Deployment {
		return getDeploy(namespace, name, v1alpha1.RevisionServingStateType(servingState), image,
			loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
	}
	svc := func(namespace, name, servingState, image string) *corev1.Service {
		return getService(namespace, name, v1alpha1.RevisionServingStateType(servingState), image,
			loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
	}
	deployAS := func(namespace, name, servingState, image string) *appsv1.Deployment {
		return getASDeploy(namespace, name, v1alpha1.RevisionServingStateType(servingState), image,
			loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
	}
	svcAS := func(namespace, name, servingState, image string) *corev1.Service {
		return getASService(namespace, name, v1alpha1.RevisionServingStateType(servingState), image,
			loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
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
			deploy("foo", "first-reconcile-var-log", "Active", "busybox"),
			svc("foo", "first-reconcile-var-log", "Active", "busybox"),
			resources.MakeFluentdConfigMap(rev("foo", "first-reconcile-var-log", "Active", "busybox"), observabilityConfig),
			deployAS("foo", "first-reconcile-var-log", "Active", "busybox"),
			svcAS("foo", "first-reconcile-var-log", "Active", "busybox"),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "first-reconcile-var-log", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "first-reconcile-var-log", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
			resources.MakeFluentdConfigMap(rev("foo", "create-configmap-failure", "Active", "busybox"), observabilityConfig),
			// We don't create the autoscaler resources if we fail to create the fluentd configmap
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: makeStatus(
				rev("foo", "create-configmap-failure", "Active", "busybox"),
				// After the first reconciliation of a Revision the status looks like this.
				v1alpha1.RevisionStatus{
					ServiceName: svc("foo", "create-configmap-failure", "Active", "busybox").Name,
					LogURL:      "http://logger.io/test-uid",
					Conditions: []v1alpha1.RevisionCondition{{
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
					Conditions: []v1alpha1.RevisionCondition{{
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
			deploy("foo", "steady-state", "Active", "busybox"),
			deployAS("foo", "steady-state", "Active", "busybox"),
			svc("foo", "steady-state", "Active", "busybox"),
			svcAS("foo", "steady-state", "Active", "busybox"),
			resources.MakeFluentdConfigMap(rev("foo", "steady-state", "Active", "busybox"), observabilityConfig),
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
					Conditions: []v1alpha1.RevisionCondition{{
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
			deploy("foo", "update-fluentd-config", "Active", "busybox"),
			deployAS("foo", "update-fluentd-config", "Active", "busybox"),
			svc("foo", "update-fluentd-config", "Active", "busybox"),
			svcAS("foo", "update-fluentd-config", "Active", "busybox"),
			&corev1.ConfigMap{
				// Use the ObjectMeta, but discard the rest.
				ObjectMeta: resources.MakeFluentdConfigMap(
					rev("foo", "update-fluentd-config", "Active", "busybox"),
					observabilityConfig,
				).ObjectMeta,
				Data: map[string]string{
					"bad key": "bad value",
				},
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// We should see a single update to the configmap we expect.
			Object: resources.MakeFluentdConfigMap(
				rev("foo", "update-fluentd-config", "Active", "busybox"),
				observabilityConfig,
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
					Conditions: []v1alpha1.RevisionCondition{{
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
			deployAS("foo", "update-configmap-failure", "Active", "busybox"),
			svc("foo", "update-configmap-failure", "Active", "busybox"),
			svcAS("foo", "update-configmap-failure", "Active", "busybox"),
			&corev1.ConfigMap{
				// Use the ObjectMeta, but discard the rest.
				ObjectMeta: resources.MakeFluentdConfigMap(rev("foo", "update-configmap-failure", "Active", "busybox"), observabilityConfig).ObjectMeta,
				Data: map[string]string{
					"bad key": "bad value",
				},
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// We should see a single update to the configmap we expect.
			Object: resources.MakeFluentdConfigMap(rev("foo", "update-configmap-failure", "Active", "busybox"), observabilityConfig),
		}},
		Key: "foo/update-configmap-failure",
	}}

	table.Test(t, func(listers *Listers, opt controller.Options) controller.Interface {
		return &Controller{
			Base:                controller.NewBase(opt, controllerAgentName, "Revisions"),
			revisionLister:      listers.GetRevisionLister(),
			buildLister:         listers.GetBuildLister(),
			deploymentLister:    listers.GetDeploymentLister(),
			serviceLister:       listers.GetK8sServiceLister(),
			endpointsLister:     listers.GetEndpointsLister(),
			configMapLister:     listers.GetConfigMapLister(),
			controllerConfig:    controllerConfig,
			networkConfig:       networkConfig,
			loggingConfig:       loggingConfig,
			observabilityConfig: observabilityConfig,
			autoscalerConfig:    autoscalerConfig,
			resolver:            &nopResolver{},
			buildtracker:        &buildTracker{builds: map[key]set{}},
		}
	})
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

// Build is a special case of resource creation because it isn't owned by
// the Revision, just tracked.
func build(namespace, name string, conds ...buildv1alpha1.BuildCondition) *buildv1alpha1.Build {
	return &buildv1alpha1.Build{
		ObjectMeta: om(namespace, name),
		Status: buildv1alpha1.BuildStatus{
			Conditions: conds,
		},
	}
}

// The input signatures of these functions should be kept in sync for readability.
func getRev(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string,
	loggingConfig *logging.Config, networkConfig *config.Network, observabilityConfig *config.Observability,
	autoscalerConfig *autoscaler.Config, controllerConfig *config.Controller) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: om(namespace, name),
		Spec: v1alpha1.RevisionSpec{
			Container:    corev1.Container{Image: image},
			ServingState: servingState,
		},
	}
}

func getDeploy(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string,
	loggingConfig *logging.Config, networkConfig *config.Network, observabilityConfig *config.Observability,
	autoscalerConfig *autoscaler.Config, controllerConfig *config.Controller) *appsv1.Deployment {

	var replicaCount int32 = 1
	if servingState == v1alpha1.RevisionServingStateReserve {
		replicaCount = 0
	}
	rev := getRev(namespace, name, servingState, image, loggingConfig, networkConfig, observabilityConfig,
		autoscalerConfig, controllerConfig)
	return resources.MakeDeployment(rev, loggingConfig, networkConfig, observabilityConfig,
		autoscalerConfig, controllerConfig, replicaCount)
}

func getService(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string,
	loggingConfig *logging.Config, networkConfig *config.Network, observabilityConfig *config.Observability,
	autoscalerConfig *autoscaler.Config, controllerConfig *config.Controller) *corev1.Service {

	rev := getRev(namespace, name, servingState, image, loggingConfig, networkConfig, observabilityConfig,
		autoscalerConfig, controllerConfig)
	return resources.MakeK8sService(rev)
}

func getEndpoints(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string,
	loggingConfig *logging.Config, networkConfig *config.Network, observabilityConfig *config.Observability,
	autoscalerConfig *autoscaler.Config, controllerConfig *config.Controller) *corev1.Endpoints {

	service := getService(namespace, name, servingState, image, loggingConfig, networkConfig, observabilityConfig,
		autoscalerConfig, controllerConfig)
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: service.Namespace,
			Name:      service.Name,
		},
	}
}

func getASDeploy(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string,
	loggingConfig *logging.Config, networkConfig *config.Network, observabilityConfig *config.Observability,
	autoscalerConfig *autoscaler.Config, controllerConfig *config.Controller) *appsv1.Deployment {

	var replicaCount int32 = 1
	if servingState == v1alpha1.RevisionServingStateReserve {
		replicaCount = 0
	}
	rev := getRev(namespace, name, servingState, image, loggingConfig, networkConfig, observabilityConfig,
		autoscalerConfig, controllerConfig)
	return resources.MakeAutoscalerDeployment(rev, controllerConfig.AutoscalerImage, replicaCount)
}

func getASService(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string,
	loggingConfig *logging.Config, networkConfig *config.Network, observabilityConfig *config.Observability,
	autoscalerConfig *autoscaler.Config, controllerConfig *config.Controller) *corev1.Service {

	rev := getRev(namespace, name, servingState, image, loggingConfig, networkConfig, observabilityConfig,
		autoscalerConfig, controllerConfig)
	return resources.MakeAutoscalerService(rev)
}

func getASEndpoints(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string,
	loggingConfig *logging.Config, networkConfig *config.Network, observabilityConfig *config.Observability,
	autoscalerConfig *autoscaler.Config, controllerConfig *config.Controller) *corev1.Endpoints {

	service := getASService(namespace, name, servingState, image, loggingConfig, networkConfig, observabilityConfig,
		autoscalerConfig, controllerConfig)
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: service.Namespace,
			Name:      service.Name,
		},
	}
}
