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
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	fakebuildclientset "github.com/knative/build/pkg/client/clientset/versioned/fake"
	"github.com/knative/serving/pkg"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging"
	. "github.com/knative/serving/pkg/logging/testing"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	. "github.com/knative/serving/pkg/controller/testing"
)

type fields struct {
	r *RevisionLister
	b *BuildLister
	d *DeploymentLister
	s *K8sServiceLister
	e *EndpointsLister
}

func (f *fields) GetRevisionLister() *RevisionLister {
	if f.r == nil {
		return &RevisionLister{}
	}
	return f.r
}

func (f *fields) GetBuildLister() *BuildLister {
	if f.b == nil {
		return &BuildLister{}
	}
	return f.b
}

func (f *fields) GetDeploymentLister() *DeploymentLister {
	if f.d == nil {
		return &DeploymentLister{}
	}
	return f.d
}

func (f *fields) GetK8sServiceLister() *K8sServiceLister {
	if f.s == nil {
		return &K8sServiceLister{}
	}
	return f.s
}

func (f *fields) GetEndpointsLister() *EndpointsLister {
	if f.e == nil {
		return &EndpointsLister{}
	}
	return f.e
}

func (f *fields) GetKubeObjects() []runtime.Object {
	var kubeObjs []runtime.Object
	for _, r := range f.GetDeploymentLister().Items {
		kubeObjs = append(kubeObjs, r)
	}
	for _, r := range f.GetK8sServiceLister().Items {
		kubeObjs = append(kubeObjs, r)
	}
	for _, r := range f.GetEndpointsLister().Items {
		kubeObjs = append(kubeObjs, r)
	}
	return kubeObjs
}

func (f *fields) GetBuildObjects() []runtime.Object {
	var buildObjs []runtime.Object
	for _, r := range f.GetBuildLister().Items {
		buildObjs = append(buildObjs, r)
	}
	return buildObjs
}

func (f *fields) GetServingObjects() []runtime.Object {
	var objs []runtime.Object
	for _, r := range f.GetRevisionLister().Items {
		objs = append(objs, r)
	}
	return objs
}

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	networkConfig := &NetworkConfig{IstioOutboundIPRanges: "*"}
	loggingConfig := &logging.Config{}
	observabilityConfig := &ObservabilityConfig{
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

	tests := []struct {
		name        string
		fields      fields
		key         string
		wantErr     bool
		wantCreates []metav1.Object
		wantUpdates []clientgotesting.UpdateActionImpl
		wantDeletes []clientgotesting.DeleteActionImpl
		wantQueue   []string
	}{{
		name: "bad workqueue key",
		// Make sure Reconcile handles bad keys.
		key: "too/many/parts",
	}, {
		name: "key not found",
		// Make sure Reconcile handles good keys that don't exist.
		key: "foo/not-found",
	}, {
		name: "first revision reconciliation",
		// Test the simplest successful reconciliation flow.
		// We feed in a well formed Revision where none of its sub-resources exist,
		// and we exect it to create them and initialize the Revision's status.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{rev("foo", "first-reconcile", "Active", "busybox")},
			},
		},
		wantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			deploy("foo", "first-reconcile", "Active", "busybox"),
			svc("foo", "first-reconcile", "Active", "busybox"),
			deployAS("foo", "first-reconcile", "Active", "busybox"),
			svcAS("foo", "first-reconcile", "Active", "busybox"),
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
		key: "foo/first-reconcile",
	}, {
		name: "stable revision reconciliation",
		// Test a simple stable reconciliation of an Active Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (immediately post-creation), and verify that no changes
		// are necessary.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
				},
			},
			d: &DeploymentLister{
				Items: []*appsv1.Deployment{
					deploy("foo", "stable-reconcile", "Active", "busybox"),
					deployAS("foo", "stable-reconcile", "Active", "busybox"),
				},
			},
			s: &K8sServiceLister{
				Items: []*corev1.Service{
					svc("foo", "stable-reconcile", "Active", "busybox"),
					svcAS("foo", "stable-reconcile", "Active", "busybox"),
				},
			},
		},
		// No changes are made to any objects.
		key: "foo/stable-reconcile",
	}, {
		name: "deactivate a revision",
		// Test the transition that's made when Reserve is set.
		// We initialize the world to a stable Active state, but make the
		// Revision's ServingState Reserve.  We then looks for the expected
		// mutations, which should include reducing the Deployments to 0 replicas
		// and deleting the Kubernetes Service resources.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
				},
			},
			d: &DeploymentLister{
				Items: []*appsv1.Deployment{
					// The Deployments match what we'd expect of an Active revision.
					deploy("foo", "deactivate", "Active", "busybox"),
					deployAS("foo", "deactivate", "Active", "busybox"),
				},
			},
			s: &K8sServiceLister{
				Items: []*corev1.Service{
					// The Services match what we'd expect of an Active revision.
					svc("foo", "deactivate", "Active", "busybox"),
					svcAS("foo", "deactivate", "Active", "busybox"),
				},
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
						Type:   "Ready",
						Status: "False",
						Reason: "Inactive",
					}},
				}),
		}, {
			Object: deploy("foo", "deactivate", "Reserve", "busybox"),
		}, {
			Object: deployAS("foo", "deactivate", "Reserve", "busybox"),
		}},
		wantDeletes: []clientgotesting.DeleteActionImpl{{
			Name: svc("foo", "deactivate", "Reserve", "busybox").Name,
		}, {
			Name: svcAS("foo", "deactivate", "Reserve", "busybox").Name,
		}},
		// We update the Deployments to have zero replicas and delete the K8s Services when we deactivate.
		key: "foo/deactivate",
	}, {
		name: "deactivated revision is stable",
		// Test a simple stable reconciliation of a Reserve Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (port-Reserve), and verify that no changes are necessary.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
							Type:   "Ready",
							Status: "False",
							Reason: "Inactive",
						}},
					}),
				},
			},
			d: &DeploymentLister{
				Items: []*appsv1.Deployment{
					// The Deployments match what we'd expect of an Reserve revision.
					deploy("foo", "stable-deactivation", "Reserve", "busybox"),
					deployAS("foo", "stable-deactivation", "Reserve", "busybox"),
				},
			},
		},
		key: "foo/stable-deactivation",
	}, {
		name: "retire a revision",
		// Test the transition that's made when Retired is set.
		// We initialize the world to a stable Active state, but make the
		// Revision's ServingState Retired.  We then looks for the expected
		// mutations, which should include the deletion of all Kubernetes
		// resources.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
				},
			},
			d: &DeploymentLister{
				Items: []*appsv1.Deployment{
					// The Deployments match what we'd expect of an Active revision.
					deploy("foo", "retire", "Active", "busybox"),
					deployAS("foo", "retire", "Active", "busybox"),
				},
			},
			s: &K8sServiceLister{
				Items: []*corev1.Service{
					// The Services match what we'd expect of an Active revision.
					svc("foo", "retire", "Active", "busybox"),
					svcAS("foo", "retire", "Active", "busybox"),
				},
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
						Type:   "Ready",
						Status: "False",
						Reason: "Inactive",
					}},
				}),
		}},
		wantDeletes: []clientgotesting.DeleteActionImpl{{
			Name: deploy("foo", "retire", "Retired", "busybox").Name,
		}, {
			Name: svc("foo", "retire", "Retired", "busybox").Name,
		}, {
			Name: deployAS("foo", "retire", "Retired", "busybox").Name,
		}, {
			Name: svcAS("foo", "retire", "Retired", "busybox").Name,
		}},
		// We delete a bunch of stuff when we retire.
		key: "foo/retire",
	}, {
		name: "retired revision is stable",
		// Test a simple stable reconciliation of a Retired Revision.
		// We feed in a Revision and the resources it controls in a steady
		// state (port-Retired), and verify that no changes are necessary.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
							Type:   "Ready",
							Status: "False",
							Reason: "Inactive",
						}},
					}),
				},
			},
		},
		key: "foo/stable-retirement",
	}, {
		name: "activate a reserve revision",
		// Test the transition that's made when Active is set.
		// We initialize the world to a stable Reserve state, but make the
		// Revision's ServingState Active.  We then look for the expected
		// mutations, which should include scaling up the Deployments to
		// 1 replica each, and recreating the Kubernetes Service resources.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
							Type:   "Ready",
							Status: "False",
							Reason: "Inactive",
						}},
					}),
				},
			},
			d: &DeploymentLister{
				Items: []*appsv1.Deployment{
					// The Deployments match what we'd expect of an Reserve revision.
					deploy("foo", "activate-revision", "Reserve", "busybox"),
					deployAS("foo", "activate-revision", "Reserve", "busybox"),
				},
			},
		},
		wantCreates: []metav1.Object{
			// Activation should recreate the K8s Services
			svc("foo", "activate-revision", "Active", "busybox"),
			svcAS("foo", "activate-revision", "Active", "busybox"),
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
		key: "foo/activate-revision",
	}, {
		name: "create resources in reserve",
		// Test a reconcile of a Revision in the Reserve state.
		// This tests the initial set of resources that we create for a Revision
		// when it is in a Reserve state and none of its resources exist.  The main
		// place we should expect this transition to happen is Retired -> Reserve.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{rev("foo", "create-in-reserve", "Reserve", "busybox")},
			},
		},
		wantCreates: []metav1.Object{
			// Only Deployments are created and they have no replicas.
			deploy("foo", "create-in-reserve", "Reserve", "busybox"),
			deployAS("foo", "create-in-reserve", "Reserve", "busybox"),
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
		key: "foo/create-in-reserve",
	}, {
		name: "endpoint is created (not ready)",
		// Test the transition when a Revision's Endpoints are created (but not yet ready)
		// This initializes the state of the world to the steady-state after a Revision's
		// first reconciliation*.  It then introduces an Endpoints resource that's not yet
		// ready.  This should result in no change, since there isn't really any new
		// information.
		//
		// * - One caveat is that we have to explicitly set LastTransitionTime to keep us
		// from thinking we've been waiting for this Endpoint since the beginning of time
		// and declaring a timeout (this is the main difference from that test below).
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
				},
			},
			d: &DeploymentLister{
				Items: []*appsv1.Deployment{
					deploy("foo", "endpoint-created-not-ready", "Active", "busybox"),
					deployAS("foo", "endpoint-created-not-ready", "Active", "busybox"),
				},
			},
			s: &K8sServiceLister{
				Items: []*corev1.Service{
					svc("foo", "endpoint-created-not-ready", "Active", "busybox"),
					svcAS("foo", "endpoint-created-not-ready", "Active", "busybox"),
				},
			},
			e: &EndpointsLister{
				Items: []*corev1.Endpoints{
					endpoints("foo", "endpoint-created-not-ready", "Active", "busybox"),
					endpointsAS("foo", "endpoint-created-not-ready", "Active", "busybox"),
				},
			},
		},
		// No updates, since the endpoint didn't have meaningful status.
		key: "foo/endpoint-created-not-ready",
	}, {
		name: "endpoint is created (timed out)",
		// Test the transition when a Revision's Endpoints aren't ready after a long period.
		// This examines the effects of Reconcile when the Endpoints exist, but we think that
		// we've been waiting since the dawn of time because we omit LastTransitionTime from
		// our Conditions.  We should see an update to put us into a ServiceTimeout state.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
				},
			},
			d: &DeploymentLister{
				Items: []*appsv1.Deployment{
					deploy("foo", "endpoint-created-timeout", "Active", "busybox"),
					deployAS("foo", "endpoint-created-timeout", "Active", "busybox"),
				},
			},
			s: &K8sServiceLister{
				Items: []*corev1.Service{
					svc("foo", "endpoint-created-timeout", "Active", "busybox"),
					svcAS("foo", "endpoint-created-timeout", "Active", "busybox"),
				},
			},
			e: &EndpointsLister{
				Items: []*corev1.Endpoints{
					endpoints("foo", "endpoint-created-timeout", "Active", "busybox"),
					endpointsAS("foo", "endpoint-created-timeout", "Active", "busybox"),
				},
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
		key: "foo/endpoint-created-timeout",
	}, {
		name: "endpoint is ready",
		// Test the transition that Reconcile makes when Endpoints become ready.
		// This puts the world into the stable post-reconcile state for an Active
		// Revision.  It then creates an Endpoints resource with active subsets.
		// This signal should make our Reconcile mark the Revision as Ready.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
				},
			},
			d: &DeploymentLister{
				Items: []*appsv1.Deployment{
					deploy("foo", "endpoint-ready", "Active", "busybox"),
					deployAS("foo", "endpoint-ready", "Active", "busybox"),
				},
			},
			s: &K8sServiceLister{
				Items: []*corev1.Service{
					svc("foo", "endpoint-ready", "Active", "busybox"),
					svcAS("foo", "endpoint-ready", "Active", "busybox"),
				},
			},
			e: &EndpointsLister{
				Items: []*corev1.Endpoints{
					addEndpoint(endpoints("foo", "endpoint-ready", "Active", "busybox")),
					addEndpoint(endpointsAS("foo", "endpoint-ready", "Active", "busybox")),
				},
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
		key: "foo/endpoint-ready",
	}, {
		name: "mutated service gets fixed",
		// Test that we correct mutations to our K8s Service resources.
		// This initializes the world to the stable post-create reconcile, and
		// adds in mutations to the K8s services that we control.  We then
		// verify that Reconcile posts the appropriate updates to correct the
		// services back to our desired specification.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
				},
			},
			d: &DeploymentLister{
				Items: []*appsv1.Deployment{
					deploy("foo", "fix-mutated-service", "Active", "busybox"),
					deployAS("foo", "fix-mutated-service", "Active", "busybox"),
				},
			},
			s: &K8sServiceLister{
				Items: []*corev1.Service{
					changeService(svc("foo", "fix-mutated-service", "Active", "busybox")),
					changeService(svcAS("foo", "fix-mutated-service", "Active", "busybox")),
				},
			},
			e: &EndpointsLister{
				Items: []*corev1.Endpoints{
					endpoints("foo", "fix-mutated-service", "Active", "busybox"),
					endpointsAS("foo", "fix-mutated-service", "Active", "busybox"),
				},
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
		key: "foo/fix-mutated-service",
	}, {
		name: "surface deployment timeout",
		// Test the propagation of ProgressDeadlineExceeded from Deployment.
		// This initializes the world to the stable state after its first reconcile,
		// but changes the user deployment to have a ProgressDeadlineExceeded
		// condition.  It then verifies that Reconcile propagates this into the
		// status of the Revision.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
				},
			},
			d: &DeploymentLister{
				Items: []*appsv1.Deployment{
					timeoutDeploy(deploy("foo", "deploy-timeout", "Active", "busybox")),
					deployAS("foo", "deploy-timeout", "Active", "busybox"),
				},
			},
			s: &K8sServiceLister{
				Items: []*corev1.Service{
					svc("foo", "deploy-timeout", "Active", "busybox"),
					svcAS("foo", "deploy-timeout", "Active", "busybox"),
				},
			},
			e: &EndpointsLister{
				Items: []*corev1.Endpoints{
					endpoints("foo", "deploy-timeout", "Active", "busybox"),
					endpointsAS("foo", "deploy-timeout", "Active", "busybox"),
				},
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
		key: "foo/deploy-timeout",
	}, {
		name: "build missing",
		// Test a Reconcile of a Revision with a Build that is not found.
		// We seed the world with a freshly created Revision that has a BuildName.
		// We then verify that a Reconcile does effectively nothing but initialize
		// the conditions of this Revision.  It is notable that unlike the tests
		// above, this will include a BuildSucceeded condition.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{addBuild(rev("foo", "missing-build", "Active", "busybox"), "the-build")},
			},
		},
		wantErr: true,
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
		key: "foo/missing-build",
	}, {
		name: "build running",
		// Test a Reconcile of a Revision with a Build that is not done.
		// We seed the world with a freshly created Revision that has a BuildName.
		// We then verify that a Reconcile does effectively nothing but initialize
		// the conditions of this Revision.  It is notable that unlike the tests
		// above, this will include a BuildSucceeded condition.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{addBuild(rev("foo", "running-build", "Active", "busybox"), "the-build")},
			},
			b: &BuildLister{
				Items: []*buildv1alpha1.Build{build("foo", "the-build")},
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
						Type:   "Ready",
						Status: "Unknown",
					}, {
						Type:   "BuildSucceeded",
						Status: "Unknown",
					}},
				}),
		}},
		key: "foo/running-build",
	}, {
		name: "build newly done",
		// Test a Reconcile of a Revision with a Build that is just done.
		// We seed the world with a freshly created Revision that has a BuildName,
		// and a Build that has a Succeeded: True status. We then verify that a
		// Reconcile toggles the BuildSucceeded status and then acts similarly to
		// the first reconcile of a BYO-Container Revision.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
					})},
			},
			b: &BuildLister{
				Items: []*buildv1alpha1.Build{
					build("foo", "the-build", buildv1alpha1.BuildCondition{
						Type:   buildv1alpha1.BuildSucceeded,
						Status: corev1.ConditionTrue,
					}),
				},
			},
		},
		wantCreates: []metav1.Object{
			// The first reconciliation of a Revision creates the following resources.
			deploy("foo", "done-build", "Active", "busybox"),
			svc("foo", "done-build", "Active", "busybox"),
			deployAS("foo", "done-build", "Active", "busybox"),
			svcAS("foo", "done-build", "Active", "busybox"),
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
		key: "foo/done-build",
	}, {
		name: "stable revision reconciliation (with build)",
		// Test a simple stable reconciliation of an Active Revision with a done Build.
		// We feed in a Revision and the resources it controls in a steady
		// state (immediately post-build completion), and verify that no changes
		// are necessary.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
				},
			},
			b: &BuildLister{
				Items: []*buildv1alpha1.Build{
					build("foo", "the-build", buildv1alpha1.BuildCondition{
						Type:   buildv1alpha1.BuildSucceeded,
						Status: corev1.ConditionTrue,
					}),
				},
			},
			d: &DeploymentLister{
				Items: []*appsv1.Deployment{
					deploy("foo", "stable-reconcile-with-build", "Active", "busybox"),
					deployAS("foo", "stable-reconcile-with-build", "Active", "busybox"),
				},
			},
			s: &K8sServiceLister{
				Items: []*corev1.Service{
					svc("foo", "stable-reconcile-with-build", "Active", "busybox"),
					svcAS("foo", "stable-reconcile-with-build", "Active", "busybox"),
				},
			},
		},
		// No changes are made to any objects.
		key: "foo/stable-reconcile-with-build",
	}, {
		name: "build newly failed",
		// Test a Reconcile of a Revision with a Build that has just failed.
		// We seed the world with a freshly created Revision that has a BuildName,
		// and a Build that has just failed. We then verify that a Reconcile toggles
		// the BuildSucceeded status and stops.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
					})},
			},
			b: &BuildLister{
				Items: []*buildv1alpha1.Build{
					build("foo", "the-build", buildv1alpha1.BuildCondition{
						Type:    buildv1alpha1.BuildSucceeded,
						Status:  corev1.ConditionFalse,
						Reason:  "SomeReason",
						Message: "This is why the build failed.",
					}),
				},
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
		key: "foo/failed-build",
	}, {
		name: "build failed stable",
		// Test a Reconcile of a Revision with a Build that has previously failed.
		// We seed the world with a Revision that has a BuildName, and a Build that
		// has failed, which has been previously reconcile. We then verify that a
		// Reconcile has nothing to change.
		fields: fields{
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{makeStatus(
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
					})},
			},
			b: &BuildLister{
				Items: []*buildv1alpha1.Build{
					build("foo", "the-build", buildv1alpha1.BuildCondition{
						Type:    buildv1alpha1.BuildSucceeded,
						Status:  corev1.ConditionFalse,
						Reason:  "SomeReason",
						Message: "This is why the build failed.",
					}),
				},
			},
		},
		key: "foo/failed-build-stable",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := fakekubeclientset.NewSimpleClientset(tt.fields.GetKubeObjects()...)
			client := fakeclientset.NewSimpleClientset(tt.fields.GetServingObjects()...)
			buildClient := fakebuildclientset.NewSimpleClientset(tt.fields.GetBuildObjects()...)
			// Set up our Controller from the fakes.
			c := &Controller{
				Base: controller.NewBase(controller.Options{
					KubeClientSet:    kubeClient,
					BuildClientSet:   buildClient,
					ServingClientSet: client,
					Logger:           TestLogger(t),
				}, controllerAgentName, "Revisions"),
				revisionLister:      tt.fields.GetRevisionLister(),
				buildLister:         tt.fields.GetBuildLister(),
				deploymentLister:    tt.fields.GetDeploymentLister(),
				serviceLister:       tt.fields.GetK8sServiceLister(),
				endpointsLister:     tt.fields.GetEndpointsLister(),
				controllerConfig:    controllerConfig,
				networkConfig:       networkConfig,
				loggingConfig:       loggingConfig,
				observabilityConfig: observabilityConfig,
				autoscalerConfig:    autoscalerConfig,
				resolver:            &nopResolver{},
				buildtracker:        &buildTracker{builds: map[key]set{}},
			}

			// Run the Reconcile we're testing.
			if err := c.Reconcile(tt.key); (err != nil) != tt.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tt.wantErr)
			}
			// Now check that the Reconcile had the desired effects.
			expectedNamespace, _, _ := cache.SplitMetaNamespaceKey(tt.key)

			c.WorkQueue.ShutDown()
			var hasQueue []string
			for {
				key, shutdown := c.WorkQueue.Get()
				if shutdown {
					break
				}
				hasQueue = append(hasQueue, key.(string))
			}
			if diff := cmp.Diff(tt.wantQueue, hasQueue); diff != "" {
				t.Errorf("unexpected queue (-want +got): %s", diff)
			}

			var createActions []clientgotesting.CreateAction
			var updateActions []clientgotesting.UpdateAction
			var deleteActions []clientgotesting.DeleteAction

			type HasActions interface {
				Actions() []clientgotesting.Action
			}
			for _, c := range []HasActions{buildClient, client, kubeClient} {
				for _, action := range c.Actions() {
					switch action.GetVerb() {
					case "create":
						createActions = append(createActions,
							action.(clientgotesting.CreateAction))
					case "update":
						updateActions = append(updateActions,
							action.(clientgotesting.UpdateAction))
					case "delete":
						deleteActions = append(deleteActions,
							action.(clientgotesting.DeleteAction))
					default:
						t.Errorf("Unexpected verb %v: %+v", action.GetVerb(), action)
					}
				}
			}

			for i, want := range tt.wantCreates {
				if i >= len(createActions) {
					t.Errorf("Missing create: %v", want)
					continue
				}
				got := createActions[i]
				if got.GetNamespace() != expectedNamespace && got.GetNamespace() != pkg.GetServingSystemNamespace() {
					t.Errorf("unexpected action[%d]: %#v", i, got)
				}
				obj := got.GetObject()
				if diff := cmp.Diff(want, obj, ignoreLastTransitionTime, safeDeployDiff); diff != "" {
					t.Errorf("unexpected create (-want +got): %s", diff)
				}
			}
			if got, want := len(createActions), len(tt.wantCreates); got > want {
				for _, extra := range createActions[want:] {
					t.Errorf("Extra create: %v", extra)
				}
			}

			for i, want := range tt.wantUpdates {
				if i >= len(updateActions) {
					t.Errorf("Missing update: %v", want.GetObject())
					continue
				}
				got := updateActions[i]
				if diff := cmp.Diff(want.GetObject(), got.GetObject(), ignoreLastTransitionTime, safeDeployDiff); diff != "" {
					t.Errorf("unexpected update (-want +got): %s", diff)
				}
			}
			if got, want := len(updateActions), len(tt.wantUpdates); got > want {
				for _, extra := range updateActions[want:] {
					t.Errorf("Extra update: %v", extra)
				}
			}

			for i, want := range tt.wantDeletes {
				if i >= len(deleteActions) {
					t.Errorf("Missing delete: %v", want)
					continue
				}
				got := deleteActions[i]
				if got.GetName() != want.Name {
					t.Errorf("unexpected delete[%d]: %#v", i, got)
				}
				if got.GetNamespace() != expectedNamespace && got.GetNamespace() != pkg.GetServingSystemNamespace() {
					t.Errorf("unexpected delete[%d]: %#v", i, got)
				}
			}
			if got, want := len(deleteActions), len(tt.wantDeletes); got > want {
				for _, extra := range deleteActions[want:] {
					t.Errorf("Extra delete: %v", extra)
				}
			}
		})
	}
}

var ignoreLastTransitionTime = cmp.FilterPath(func(p cmp.Path) bool {
	return strings.HasSuffix(p.String(), "LastTransitionTime.Time")
}, cmp.Ignore())

var safeDeployDiff = cmpopts.IgnoreUnexported(resource.Quantity{})

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
	loggingConfig *logging.Config, networkConfig *NetworkConfig, observabilityConfig *ObservabilityConfig,
	autoscalerConfig *autoscaler.Config, controllerConfig *ControllerConfig) *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: om(namespace, name),
		Spec: v1alpha1.RevisionSpec{
			Container:    corev1.Container{Image: image},
			ServingState: servingState,
		},
	}
}

func getDeploy(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string,
	loggingConfig *logging.Config, networkConfig *NetworkConfig, observabilityConfig *ObservabilityConfig,
	autoscalerConfig *autoscaler.Config, controllerConfig *ControllerConfig) *appsv1.Deployment {

	var replicaCount int32 = 1
	if servingState == v1alpha1.RevisionServingStateReserve {
		replicaCount = 0
	}
	rev := getRev(namespace, name, servingState, image, loggingConfig, networkConfig, observabilityConfig,
		autoscalerConfig, controllerConfig)
	return MakeServingDeployment(rev, loggingConfig, networkConfig, observabilityConfig,
		autoscalerConfig, controllerConfig, replicaCount)
}

func getService(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string,
	loggingConfig *logging.Config, networkConfig *NetworkConfig, observabilityConfig *ObservabilityConfig,
	autoscalerConfig *autoscaler.Config, controllerConfig *ControllerConfig) *corev1.Service {

	rev := getRev(namespace, name, servingState, image, loggingConfig, networkConfig, observabilityConfig,
		autoscalerConfig, controllerConfig)
	return MakeRevisionK8sService(rev)
}

func getEndpoints(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string,
	loggingConfig *logging.Config, networkConfig *NetworkConfig, observabilityConfig *ObservabilityConfig,
	autoscalerConfig *autoscaler.Config, controllerConfig *ControllerConfig) *corev1.Endpoints {

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
	loggingConfig *logging.Config, networkConfig *NetworkConfig, observabilityConfig *ObservabilityConfig,
	autoscalerConfig *autoscaler.Config, controllerConfig *ControllerConfig) *appsv1.Deployment {

	var replicaCount int32 = 1
	if servingState == v1alpha1.RevisionServingStateReserve {
		replicaCount = 0
	}
	rev := getRev(namespace, name, servingState, image, loggingConfig, networkConfig, observabilityConfig,
		autoscalerConfig, controllerConfig)
	return MakeServingAutoscalerDeployment(rev, controllerConfig.AutoscalerImage, replicaCount)
}

func getASService(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string,
	loggingConfig *logging.Config, networkConfig *NetworkConfig, observabilityConfig *ObservabilityConfig,
	autoscalerConfig *autoscaler.Config, controllerConfig *ControllerConfig) *corev1.Service {

	rev := getRev(namespace, name, servingState, image, loggingConfig, networkConfig, observabilityConfig,
		autoscalerConfig, controllerConfig)
	return MakeServingAutoscalerService(rev)
}

func getASEndpoints(namespace, name string, servingState v1alpha1.RevisionServingStateType, image string,
	loggingConfig *logging.Config, networkConfig *NetworkConfig, observabilityConfig *ObservabilityConfig,
	autoscalerConfig *autoscaler.Config, controllerConfig *ControllerConfig) *corev1.Endpoints {

	service := getASService(namespace, name, servingState, image, loggingConfig, networkConfig, observabilityConfig,
		autoscalerConfig, controllerConfig)
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: service.Namespace,
			Name:      service.Name,
		},
	}
}
