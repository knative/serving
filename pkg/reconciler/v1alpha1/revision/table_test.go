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
	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	kpav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	. "github.com/knative/serving/pkg/reconciler/testing"
)

var allDeploying = []v1alpha1.RevisionCondition{{
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
}}

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
	deploy := func(rev *v1alpha1.Revision) *appsv1.Deployment {
		return resources.MakeDeployment(rev, loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
	}
	kpa := func(rev *v1alpha1.Revision) *kpav1alpha1.PodAutoscaler {
		return resources.MakeKPA(rev)
	}
	svc := func(rev *v1alpha1.Revision) *corev1.Service {
		return resources.MakeK8sService(rev)
	}
	endpoints := func(rev *v1alpha1.Revision) *corev1.Endpoints {
		return &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: rev.Namespace,
				Name:      names.K8sService(rev),
			},
		}
	}

	table := TableTest{{
		Name: "bad workqueue key",
		// Make sure Reconcile handles bad keys.
		Key: "too/many/parts",
	}, {
		Name: "key not found",
		// Make sure Reconcile handles good keys that don't exist.
		Key: "foo/not-found",
	}, BuildRow(func() TableRow {
		input := rev("foo", "first-reconcile", "Active", "busybox")
		// After the first reconciliation the input looks like this.
		output := makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions:  allDeploying,
		})

		// Test the simplest successful reconciliation flow.
		// We feed in a well formed Revision where none of its sub-resources exist,
		// and we exect it to create them and initialize the Revision's status.
		return TableRow{
			Name:        "first revision reconciliation",
			Key:         KeyOrDie(input),
			Objects:     []runtime.Object{input},
			WantCreates: []metav1.Object{kpa(input), deploy(input), svc(input)},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "update-status-failure", "Active", "busybox")
		output := makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions:  allDeploying,
		})

		// This starts from the first reconciliation case above and induces a failure
		// updating the revision status.
		return TableRow{
			Name:         "failure updating revision status",
			Key:          KeyOrDie(input),
			WantErr:      true,
			WithReactors: []clientgotesting.ReactionFunc{InduceFailure("update", "revisions")},
			Objects:      []runtime.Object{input, kpa(input)},
			WantCreates:  []metav1.Object{deploy(input), svc(input)},
			WantUpdates:  []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "create-kpa-failure", "Active", "busybox")
		output := makeStatus(input, v1alpha1.RevisionStatus{
			LogURL:      "http://logger.io/test-uid",
			ServiceName: names.K8sService(input),
			Conditions: []v1alpha1.RevisionCondition{{
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
		})

		// This starts from the first reconciliation case above and induces a failure
		// creating the kpa.
		return TableRow{
			Name:         "failure creating kpa",
			Key:          KeyOrDie(input),
			WantErr:      true,
			WithReactors: []clientgotesting.ReactionFunc{InduceFailure("create", "podautoscalers")},
			Objects:      []runtime.Object{input},
			WantCreates:  []metav1.Object{kpa(input), deploy(input), svc(input)},
			WantUpdates:  []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "create-user-deploy-failure", "Active", "busybox")
		output := makeStatus(input, v1alpha1.RevisionStatus{
			LogURL: "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})

		// This starts from the first reconciliation case above and induces a failure
		// creating the user's deployment
		return TableRow{
			Name:         "failure creating user deployment",
			Key:          KeyOrDie(input),
			WantErr:      true,
			WithReactors: []clientgotesting.ReactionFunc{InduceFailure("create", "deployments")},
			Objects:      []runtime.Object{input, kpa(input)},
			WantCreates:  []metav1.Object{deploy(input)},
			WantUpdates:  []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "create-user-service-failure", "Active", "busybox")
		output := makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})

		// This starts from the first reconciliation case above and induces a failure
		// creating the user's service.
		return TableRow{
			Name:         "failure creating user service",
			Key:          KeyOrDie(input),
			WantErr:      true,
			WithReactors: []clientgotesting.ReactionFunc{InduceFailure("create", "services")},
			Objects:      []runtime.Object{input, kpa(input)},
			WantCreates:  []metav1.Object{deploy(input), svc(input)},
			WantUpdates:  []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "stable-reconcile", "Active", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions:  allDeploying,
		})

		// Test a simple stable reconciliation of an Active Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (immediately post-creation), and verify that no changes
		// are necessary.
		return TableRow{
			Name:    "stable revision reconciliation",
			Key:     KeyOrDie(input),
			Objects: []runtime.Object{input, kpa(input), deploy(input), svc(input)},
			// No changes are made to any objects.
		}
	}), BuildRow(func() TableRow {
		// The revision has been set to Deactivated, but all of the objects
		// reflect being Active.
		input := rev("foo", "deactivate", "Reserve", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions:  allDeploying,
		})
		// The active version of the revision, off of which to base initial
		// subresource states.
		active := rev("foo", "deactivate", "Active", "busybox")

		// Test the transition that's made when Reserve is set.
		// We initialize the world to a stable Active state, but make the
		// Revision's ServingState Reserve.  We then look for the expected
		// mutations, which should be the KPA's serving state being set to
		// Reserve.
		return TableRow{
			Name:        "deactivate a revision",
			Key:         KeyOrDie(input),
			Objects:     []runtime.Object{input, kpa(active), deploy(active), svc(active)},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: kpa(input)}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "update-kpa-failure", "Reserve", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions:  allDeploying,
		})
		// The active version of the revision, off of which to base initial
		// subresource states.
		active := rev("foo", "update-kpa-failure", "Active", "busybox")

		// Test that we return a reconciliation error when we hit an error updating the KPA subresource.
		return TableRow{
			Name: "failure updating kpa",
			Key:  KeyOrDie(input),
			// Induce a failure updating the kpa
			WantErr:      true,
			WithReactors: []clientgotesting.ReactionFunc{InduceFailure("update", "podautoscalers")},
			// Create the KPA from an active version of the revision to trigger an update.
			Objects: []runtime.Object{input, kpa(active), deploy(input), svc(input)},
			// The update we expect to see (and that we make fail).
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: kpa(input)}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "stable-deactivation", "Reserve", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions:  allDeploying,
		})

		// Test a simple stable reconciliation of a Reserve Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (port-Reserve), and verify that no changes are necessary.
		return TableRow{
			Name:    "deactivated revision is stable",
			Key:     KeyOrDie(input),
			Objects: []runtime.Object{input, kpa(input), deploy(input), svc(input)},
		}
	}), BuildRow(func() TableRow {
		reserve := rev("foo", "activate-revision", "Reserve", "busybox")
		input := rev("foo", "activate-revision", "Active", "busybox")
		// The status and state of the world reflect a Reserve Revision,
		// but it has a ServingState of Active.
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(reserve),
			LogURL:      "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
				Type:   "Active",
				Status: "False",
			}, {
				Type:   "ResourcesAvailable",
				Status: "Unknown",
				Reason: "Updating",
			}, {
				Type:   "ContainerHealthy",
				Status: "Unknown",
				Reason: "Updating",
			}, {
				Type:   "Ready",
				Status: "False",
			}},
		})
		output := makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions:  allDeploying,
		})

		// Test the transition that's made when Active is set.
		// We initialize the world to a stable Reserve state, but make the
		// Revision's ServingState Active.  We then look for the expected
		// mutations, which should include scaling up the Deployments to
		// 1 replica each, and recreating the Kubernetes Service resources.
		return TableRow{
			Name:        "activate a reserve revision",
			Key:         KeyOrDie(input),
			Objects:     []runtime.Object{input, kpa(reserve), deploy(reserve), svc(reserve)},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: kpa(input)}, {Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "create-in-reserve", "Reserve", "busybox")
		output := makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions:  allDeploying,
		})

		// Test a reconcile of a Revision in the Reserve state.
		// This tests the initial set of resources that we create for a Revision
		// when it is in a Reserve state and none of its resources exist.  The main
		// place we should expect this transition to happen is Retired -> Reserve.
		return TableRow{
			Name:        "create resources in reserve",
			Key:         KeyOrDie(input),
			Objects:     []runtime.Object{input},
			WantCreates: []metav1.Object{kpa(input), deploy(input), svc(input)},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "endpoint-created-not-ready", "Active", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})

		// Test the transition when a Revision's Endpoints are created (but not yet ready)
		// This initializes the state of the world to the steady-state after a Revision's
		// first reconciliation*.  It then introduces an Endpoints resource that's not yet
		// ready.  This should result in no change, since there isn't really any new
		// information.
		//
		// * - One caveat is that we have to explicitly set LastTransitionTime to keep us
		// from thinking we've been waiting for this Endpoint since the beginning of time
		// and declaring a timeout (this is the main difference from that test below).
		return TableRow{
			Name:    "endpoint is created (not ready)",
			Key:     KeyOrDie(input),
			Objects: []runtime.Object{input, kpa(input), deploy(input), svc(input), endpoints(input)},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "endpoint-created-timeout", "Active", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})
		output := input.DeepCopy()
		output.Status.MarkServiceTimeout()

		// Test the transition when a Revision's Endpoints aren't ready after a long period.
		// This examines the effects of Reconcile when the Endpoints exist, but we think that
		// we've been waiting since the dawn of time because we omit LastTransitionTime from
		// our Conditions.  We should see an update to put us into a ServiceTimeout state.
		return TableRow{
			Name:        "endpoint is created (timed out)",
			Key:         KeyOrDie(input),
			Objects:     []runtime.Object{input, kpa(input), deploy(input), svc(input), endpoints(input)},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "endpoint-ready", "Active", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions:  allDeploying,
		})
		output := input.DeepCopy()
		output.Status.MarkActive()
		output.Status.MarkContainerHealthy()
		output.Status.MarkResourcesAvailable()

		// Test the transition that Reconcile makes when Endpoints become ready.
		// This puts the world into the stable post-reconcile state for an Active
		// Revision.  It then creates an Endpoints resource with active subsets.
		// This signal should make our Reconcile mark the Revision as Ready.
		return TableRow{
			Name: "endpoint and kpa are ready",
			Key:  KeyOrDie(input),
			Objects: []runtime.Object{input, deploy(input), svc(input), addEndpoint(endpoints(input)),
				addKPAStatus(kpa(input), kpav1alpha1.PodAutoscalerStatus{
					Conditions: []kpav1alpha1.PodAutoscalerCondition{{
						Type:   "Active",
						Status: "True",
					}, {
						Type:   "Ready",
						Status: "True",
					}},
				}),
			},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "kpa-not-ready", "Active", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})
		output := input.DeepCopy()
		output.Status.MarkActivating("Something", "This is something longer")

		// Test propagating the KPA status to the Revision.
		return TableRow{
			Name: "kpa not ready",
			Key:  KeyOrDie(input),
			Objects: []runtime.Object{input, deploy(input), svc(input), addEndpoint(endpoints(input)),
				addKPAStatus(kpa(input), kpav1alpha1.PodAutoscalerStatus{
					Conditions: []kpav1alpha1.PodAutoscalerCondition{{
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
			},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "kpa-inactive", "Active", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})
		output := input.DeepCopy()
		output.Status.MarkInactive("NoTraffic", "This thing is inactive.")

		// Test propagating the inactivity signal from the KPA to the Revision.
		return TableRow{
			Name: "kpa inactive",
			Key:  KeyOrDie(input),
			Objects: []runtime.Object{input, deploy(input), svc(input), addEndpoint(endpoints(input)),
				addKPAStatus(kpa(input), kpav1alpha1.PodAutoscalerStatus{
					Conditions: []kpav1alpha1.PodAutoscalerCondition{{
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
			},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "fix-mutated-service", "Active", "busybox")
		output := makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})

		// Test that we correct mutations to our K8s Service resources.
		// This initializes the world to the stable post-create reconcile, and
		// adds in mutations to the K8s services that we control.  We then
		// verify that Reconcile posts the appropriate updates to correct the
		// services back to our desired specification.
		return TableRow{
			Name:        "mutated service gets fixed",
			Key:         KeyOrDie(input),
			Objects:     []runtime.Object{input, kpa(input), deploy(input), changeService(svc(input)), endpoints(input)},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: output}, {Object: svc(input)}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "update-user-svc-failure", "Active", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
			}},
		})

		// Induce a failure updating the user service.
		return TableRow{
			Name:         "failure updating user service",
			Key:          KeyOrDie(input),
			WantErr:      true,
			WithReactors: []clientgotesting.ReactionFunc{InduceFailure("update", "services")},
			Objects: []runtime.Object{input, kpa(input), deploy(input),
				changeService(svc(input)), endpoints(input)},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: svc(input)}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "deploy-timeout", "Active", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})
		output := input.DeepCopy()
		output.Status.MarkProgressDeadlineExceeded("Unable to create pods for more than 120 seconds.")

		// Test the propagation of ProgressDeadlineExceeded from Deployment.
		// This initializes the world to the stable state after its first reconcile,
		// but changes the user deployment to have a ProgressDeadlineExceeded
		// condition.  It then verifies that Reconcile propagates this into the
		// status of the Revision.
		return TableRow{
			Name: "surface deployment timeout",
			Key:  KeyOrDie(input),
			Objects: []runtime.Object{input, kpa(input), timeoutDeploy(deploy(input)),
				svc(input), endpoints(input)},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := addBuild(rev("foo", "missing-build", "Active", "busybox"))
		output := makeStatus(input, v1alpha1.RevisionStatus{
			LogURL: "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})

		// Test a Reconcile of a Revision with a Build that is not found.
		// We seed the world with a freshly created Revision that has a BuildName.
		// We then verify that a Reconcile does effectively nothing but initialize
		// the conditions of this Revision.  It is notable that unlike the tests
		// above, this will include a BuildSucceeded condition.
		return TableRow{
			Name:        "build missing",
			Key:         KeyOrDie(input),
			Objects:     []runtime.Object{input},
			WantErr:     true,
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := addBuild(rev("foo", "running-build", "Active", "busybox"))
		output := makeStatus(input, v1alpha1.RevisionStatus{
			LogURL: "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})

		// Test a Reconcile of a Revision with a Build that is not done.
		// We seed the world with a freshly created Revision that has a BuildName.
		// We then verify that a Reconcile does effectively nothing but initialize
		// the conditions of this Revision.  It is notable that unlike the tests
		// above, this will include a BuildSucceeded condition.
		return TableRow{
			Name: "build running",
			Key:  KeyOrDie(input),
			Objects: []runtime.Object{input, build(input, buildv1alpha1.BuildCondition{
				Type:   buildv1alpha1.BuildSucceeded,
				Status: corev1.ConditionUnknown,
			})},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := addBuild(rev("foo", "done-build", "Active", "busybox"))
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})
		bld := build(input, buildv1alpha1.BuildCondition{
			Type:   buildv1alpha1.BuildSucceeded,
			Status: corev1.ConditionTrue,
		})
		output := input.DeepCopy()
		output.Status.PropagateBuildStatus(bld.Status)
		output.Status.MarkActivating("Deploying", "")
		output.Status.MarkDeploying("Deploying")

		// Test a Reconcile of a Revision with a Build that is just done.
		// We seed the world with a freshly created Revision that has a BuildName,
		// and a Build that has a Succeeded: True status. We then verify that a
		// Reconcile toggles the BuildSucceeded status and then acts similarly to
		// the first reconcile of a BYO-Container Revision.
		return TableRow{
			Name:        "build newly done",
			Key:         KeyOrDie(input),
			Objects:     []runtime.Object{input, bld},
			WantCreates: []metav1.Object{kpa(input), deploy(input), svc(input)},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := addBuild(rev("foo", "stable-reconcile-with-build", "Active", "busybox"))
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})

		// Test a simple stable reconciliation of an Active Revision with a done Build.
		// We feed in a Revision and the resources it controls in a steady
		// state (immediately post-build completion), and verify that no changes
		// are necessary.
		return TableRow{
			Name: "stable revision reconciliation (with build)",
			Key:  KeyOrDie(input),
			Objects: []runtime.Object{input, kpa(input), deploy(input), svc(input),
				build(input, buildv1alpha1.BuildCondition{
					Type:   buildv1alpha1.BuildSucceeded,
					Status: corev1.ConditionTrue,
				}),
			},
		}
	}), BuildRow(func() TableRow {
		input := addBuild(rev("foo", "failed-build", "Active", "busybox"))
		input = makeStatus(input, v1alpha1.RevisionStatus{
			LogURL: "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})
		bld := build(input, buildv1alpha1.BuildCondition{
			Type:    buildv1alpha1.BuildSucceeded,
			Status:  corev1.ConditionFalse,
			Reason:  "SomeReason",
			Message: "This is why the build failed.",
		})
		output := input.DeepCopy()
		output.Status.PropagateBuildStatus(bld.Status)

		// Test a Reconcile of a Revision with a Build that has just failed.
		// We seed the world with a freshly created Revision that has a BuildName,
		// and a Build that has just failed. We then verify that a Reconcile toggles
		// the BuildSucceeded status and stops.
		return TableRow{
			Name:        "build newly failed",
			Key:         KeyOrDie(input),
			Objects:     []runtime.Object{input, bld},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := addBuild(rev("foo", "failed-build-stable", "Active", "busybox"))
		input = makeStatus(input, v1alpha1.RevisionStatus{
			LogURL: "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})

		// Test a Reconcile of a Revision with a Build that has previously failed.
		// We seed the world with a Revision that has a BuildName, and a Build that
		// has failed, which has been previously reconcile. We then verify that a
		// Reconcile has nothing to change.
		return TableRow{
			Name: "build failed stable",
			Key:  KeyOrDie(input),
			Objects: []runtime.Object{input, build(input, buildv1alpha1.BuildCondition{
				Type:    buildv1alpha1.BuildSucceeded,
				Status:  corev1.ConditionFalse,
				Reason:  "SomeReason",
				Message: "This is why the build failed.",
			})},
		}
	})}

	table.Test(t, MakeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		return &Reconciler{
			Base:                reconciler.NewBase(opt, controllerAgentName),
			revisionLister:      listers.GetRevisionLister(),
			kpaLister:           listers.GetKPALister(),
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
	}))
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
	deploy := func(rev *v1alpha1.Revision) *appsv1.Deployment {
		return resources.MakeDeployment(rev, loggingConfig, networkConfig, observabilityConfig,
			autoscalerConfig, controllerConfig)
	}
	kpa := func(rev *v1alpha1.Revision) *kpav1alpha1.PodAutoscaler {
		return resources.MakeKPA(rev)
	}
	svc := func(rev *v1alpha1.Revision) *corev1.Service {
		return resources.MakeK8sService(rev)
	}
	cm := func(rev *v1alpha1.Revision) *corev1.ConfigMap {
		return resources.MakeFluentdConfigMap(rev, observabilityConfig)
	}

	table := TableTest{BuildRow(func() TableRow {
		input := rev("foo", "first-reconcile-var-log", "Active", "busybox")
		output := makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions:  allDeploying,
		})

		// Test a successful reconciliation flow with /var/log enabled.
		// We feed in a well formed Revision where none of its sub-resources exist,
		// and we exect it to create them and initialize the Revision's status.
		// This is similar to "first-reconcile", but should also create a fluentd configmap.
		return TableRow{
			Name:        "first revision reconciliation (with /var/log enabled)",
			Key:         KeyOrDie(input),
			Objects:     []runtime.Object{input},
			WantCreates: []metav1.Object{kpa(input), deploy(input), svc(input), cm(input)},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "create-configmap-failure", "Active", "busybox")
		output := makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions: []v1alpha1.RevisionCondition{{
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
		})

		// Induce a failure creating the fluentd configmap.
		return TableRow{
			Name:         "failure creating fluentd configmap",
			Key:          KeyOrDie(input),
			WantErr:      true,
			WithReactors: []clientgotesting.ReactionFunc{InduceFailure("create", "configmaps")},
			Objects:      []runtime.Object{input},
			WantCreates:  []metav1.Object{deploy(input), svc(input), cm(input)},
			WantUpdates:  []clientgotesting.UpdateActionImpl{{Object: output}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "steady-state", "Active", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions:  allDeploying,
		})

		// Verify that after creating the things from an initial reconcile that we're stable.
		return TableRow{
			Name:    "steady state after initial creation",
			Key:     KeyOrDie(input),
			Objects: []runtime.Object{input, kpa(input), deploy(input), svc(input), cm(input)},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "update-fluentd-config", "Active", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions:  allDeploying,
		})

		// Verify that we fix a bad fluentd configmap
		return TableRow{
			Name: "update a bad fluentd configmap",
			Key:  KeyOrDie(input),
			Objects: []runtime.Object{input, kpa(input), deploy(input), svc(input),
				&corev1.ConfigMap{
					// Use the ObjectMeta, but discard the rest.
					ObjectMeta: cm(input).ObjectMeta,
					Data: map[string]string{
						"bad key": "bad value",
					},
				},
			},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: cm(input)}},
		}
	}), BuildRow(func() TableRow {
		input := rev("foo", "update-configmap-failure", "Active", "busybox")
		input = makeStatus(input, v1alpha1.RevisionStatus{
			ServiceName: names.K8sService(input),
			LogURL:      "http://logger.io/test-uid",
			Conditions:  allDeploying,
		})

		// Induce a failure trying to update the fluentd configmap
		return TableRow{
			Name:         "failure updating fluentd configmap",
			Key:          KeyOrDie(input),
			WantErr:      true,
			WithReactors: []clientgotesting.ReactionFunc{InduceFailure("update", "configmaps")},
			Objects: []runtime.Object{input, deploy(input), svc(input), &corev1.ConfigMap{
				// Use the ObjectMeta, but discard the rest.
				ObjectMeta: cm(input).ObjectMeta,
				Data: map[string]string{
					"bad key": "bad value",
				},
			}},
			WantUpdates: []clientgotesting.UpdateActionImpl{{Object: cm(input)}},
		}
	})}

	table.Test(t, MakeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		return &Reconciler{
			Base:                reconciler.NewBase(opt, controllerAgentName),
			revisionLister:      listers.GetRevisionLister(),
			kpaLister:           listers.GetKPALister(),
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
	}))
}

func om(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		UID:       "test-uid",
	}
}

func makeStatus(rev *v1alpha1.Revision, status v1alpha1.RevisionStatus) *v1alpha1.Revision {
	rev = rev.DeepCopy()
	rev.Status = status
	return rev
}

func addBuild(rev *v1alpha1.Revision) *v1alpha1.Revision {
	rev.Spec.BuildName = rev.Name
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

func build(rev *v1alpha1.Revision, conds ...buildv1alpha1.BuildCondition) *buildv1alpha1.Build {
	return &buildv1alpha1.Build{
		ObjectMeta: om(rev.Namespace, rev.Name),
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
