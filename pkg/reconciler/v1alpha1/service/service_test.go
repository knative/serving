/*
Copyright 2018 The Knative Authors

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

package service

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	fakesharedclientset "github.com/knative/pkg/client/clientset/versioned/fake"
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources"

	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
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
		Name: "incomplete service",
		Objects: []runtime.Object{
			// There is no spec.{runLatest,pinned} in this Service to
			// trigger the error condition.
			svc("incomplete", "foo", WithInitSvcConditions),
		},
		Key:     "foo/incomplete",
		WantErr: true,
	}, {
		Name: "runLatest - create route and service",
		Objects: []runtime.Object{
			svc("run-latest", "foo", WithRunLatestRollout),
		},
		Key: "foo/run-latest",
		WantCreates: []metav1.Object{
			config("run-latest", "foo", WithRunLatestRollout),
			route("run-latest", "foo", WithRunLatestRollout),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svc("run-latest", "foo", WithRunLatestRollout,
				// The first reconciliation will initialize the status conditions.
				WithInitSvcConditions),
		}},
	}, {
		Name: "pinned - create route and service",
		Objects: []runtime.Object{
			svc("pinned", "foo", WithPinnedRollout("pinned-0001")),
		},
		Key: "foo/pinned",
		WantCreates: []metav1.Object{
			config("pinned", "foo", WithPinnedRollout("pinned-0001")),
			route("pinned", "foo", WithPinnedRollout("pinned-0001")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svc("pinned", "foo", WithPinnedRollout("pinned-0001"),
				// The first reconciliation will initialize the status conditions.
				WithInitSvcConditions),
		}},
	}, {
		Name: "release - create route and service",
		Objects: []runtime.Object{
			svc("release", "foo", WithReleaseRollout("release-00001", "release-00002")),
		},
		Key: "foo/release",
		WantCreates: []metav1.Object{
			config("release", "foo", WithReleaseRollout("release-00001", "release-00002")),
			route("release", "foo", WithReleaseRollout("release-00001", "release-00002")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svc("release", "foo", WithReleaseRollout("release-00001", "release-00002"),
				// The first reconciliation will initialize the status conditions.
				WithInitSvcConditions),
		}},
	}, {
		Name: "manual- no creates",
		Objects: []runtime.Object{
			svc("manual", "foo", WithManualRollout),
		},
		Key: "foo/manual",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svc("manual", "foo", WithManualRollout,
				// The first reconciliation will initialize the status conditions.
				WithManualStatus),
		}},
	}, {
		Name: "runLatest - no updates",
		Objects: []runtime.Object{
			svc("no-updates", "foo", WithRunLatestRollout, WithInitSvcConditions),
			route("no-updates", "foo", WithRunLatestRollout),
			config("no-updates", "foo", WithRunLatestRollout),
		},
		Key: "foo/no-updates",
	}, {
		Name: "runLatest - update route and service",
		Objects: []runtime.Object{
			svc("update-route-and-config", "foo", WithRunLatestRollout, WithInitSvcConditions),
			// Mutate the Config/Route to have a different body than we want.
			config("update-route-and-config", "foo", WithRunLatestRollout,
				// Change the concurrency model to ensure it is corrected.
				WithConfigConcurrencyModel("Single")),
			route("update-route-and-config", "foo", WithRunLatestRollout, MutateRoute),
		},
		Key: "foo/update-route-and-config",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: config("update-route-and-config", "foo", WithRunLatestRollout),
		}, {
			Object: route("update-route-and-config", "foo", WithRunLatestRollout),
		}},
	}, {
		Name: "runLatest - bad config update",
		Objects: []runtime.Object{
			// There is no spec.{runLatest,pinned} in this Service, which triggers the error
			// path updating Configuration.
			svc("bad-config-update", "foo", WithInitSvcConditions),
			config("bad-config-update", "foo", WithRunLatestRollout,
				// Change the concurrency model to ensure it is corrected.
				WithConfigConcurrencyModel("Single")),
			route("bad-config-update", "foo", WithRunLatestRollout, MutateRoute),
		},
		Key:     "foo/bad-config-update",
		WantErr: true,
	}, {
		Name: "runLatest - route creation failure",
		// Induce a failure during route creation
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "routes"),
		},
		Objects: []runtime.Object{
			svc("create-route-failure", "foo", WithRunLatestRollout),
		},
		Key: "foo/create-route-failure",
		WantCreates: []metav1.Object{
			config("create-route-failure", "foo", WithRunLatestRollout),
			route("create-route-failure", "foo", WithRunLatestRollout),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svc("create-route-failure", "foo", WithRunLatestRollout,
				// First reconcile initializes conditions.
				WithInitSvcConditions),
		}},
	}, {
		Name: "runLatest - configuration creation failure",
		// Induce a failure during configuration creation
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "configurations"),
		},
		Objects: []runtime.Object{
			svc("create-config-failure", "foo", WithRunLatestRollout),
		},
		Key: "foo/create-config-failure",
		WantCreates: []metav1.Object{
			config("create-config-failure", "foo", WithRunLatestRollout),
			// We don't get to creating the Route.
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svc("create-config-failure", "foo", WithRunLatestRollout,
				// First reconcile initializes conditions.
				WithInitSvcConditions),
		}},
	}, {
		Name: "runLatest - update route failure",
		// Induce a failure updating the route
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "routes"),
		},
		Objects: []runtime.Object{
			svc("update-route-failure", "foo", WithRunLatestRollout, WithInitSvcConditions),
			// Mutate the Route to have an unexpected body to trigger an update.
			route("update-route-failure", "foo", WithRunLatestRollout, MutateRoute),
			config("update-route-failure", "foo", WithRunLatestRollout),
		},
		Key: "foo/update-route-failure",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: route("update-route-failure", "foo", WithRunLatestRollout),
		}},
	}, {
		Name: "runLatest - update config failure",
		// Induce a failure updating the config
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "configurations"),
		},
		Objects: []runtime.Object{
			svc("update-config-failure", "foo", WithRunLatestRollout, WithInitSvcConditions),
			route("update-config-failure", "foo", WithRunLatestRollout),
			// Mutate the Config to have an unexpected body to trigger an update.
			config("update-config-failure", "foo", WithRunLatestRollout,
				// Change the concurrency model to ensure it is corrected.
				WithConfigConcurrencyModel("Single")),
		},
		Key: "foo/update-config-failure",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: config("update-config-failure", "foo", WithRunLatestRollout),
		}},
	}, {
		Name: "runLatest - failure updating service status",
		// Induce a failure updating the service status.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "services"),
		},
		Objects: []runtime.Object{
			svc("run-latest", "foo", WithRunLatestRollout),
		},
		Key: "foo/run-latest",
		WantCreates: []metav1.Object{
			config("run-latest", "foo", WithRunLatestRollout),
			route("run-latest", "foo", WithRunLatestRollout),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svc("run-latest", "foo", WithRunLatestRollout,
				// We attempt to update the Service to initialize its
				// conditions, which is where we induce the failure.
				WithInitSvcConditions),
		}},
	}, {
		Name: "runLatest - route and config ready, propagate ready",
		// When both route and config are ready, the service should become ready.
		Objects: []runtime.Object{
			svc("all-ready", "foo", WithRunLatestRollout, WithInitSvcConditions),
			route("all-ready", "foo", WithRunLatestRollout, RouteReady),
			config("all-ready", "foo", WithRunLatestRollout, WithGeneration(1),
				// These turn a Configuration to Ready=true
				WithLatestCreated, WithLatestReady),
		},
		Key: "foo/all-ready",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svc("all-ready", "foo", WithRunLatestRollout,
				// When the Route and Configuration come back with ready
				// our initial conditions transition to Ready=True.
				// TODO(mattmoor): Add Latest{Created,Ready}
				WithReadyRoute, WithReadyConfig("all-ready-00001")),
		}},
	}, {
		Name: "runLatest - config fails, propagate failure",
		// When config fails, the service should fail.
		Objects: []runtime.Object{
			svc("config-fails", "foo", WithRunLatestRollout, WithInitSvcConditions),
			route("config-fails", "foo", WithRunLatestRollout, RouteReady),
			config("config-fails", "foo", WithRunLatestRollout, WithGeneration(1),
				WithLatestCreated, MarkLatestCreatedFailed("blah")),
		},
		Key: "foo/config-fails",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svc("config-fails", "foo", WithRunLatestRollout, WithInitSvcConditions,
				// When the Route is Ready, and the Configuration has failed,
				// we expect the following changes to our status conditions.
				WithReadyRoute, WithFailedConfig(
					"config-fails-00001", "RevisionFailed", "blah")),
		}},
	}, {
		Name: "runLatest - route fails, propagate failure",
		// When route fails, the service should fail.
		Objects: []runtime.Object{
			svc("route-fails", "foo", WithRunLatestRollout, WithInitSvcConditions),
			route("route-fails", "foo", WithRunLatestRollout,
				RouteFailed("Propagate me, please", "")),
			config("route-fails", "foo", WithRunLatestRollout, WithGeneration(1),
				// These turn a Configuration to Ready=true
				WithLatestCreated, WithLatestReady),
		},
		Key: "foo/route-fails",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svc("route-fails", "foo", WithRunLatestRollout, WithInitSvcConditions,
				// When the Configuration is Ready, and the Route has failed,
				// we expect the following changed to our status conditions.
				WithReadyConfig("route-fails-00001"),
				WithFailedRoute("Propagate me, please", "")),
		}},
	}}

	table.Test(t, MakeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		return &Reconciler{
			Base:                reconciler.NewBase(opt, controllerAgentName),
			serviceLister:       listers.GetServiceLister(),
			configurationLister: listers.GetConfigurationLister(),
			routeLister:         listers.GetRouteLister(),
		}
	}))
}

func TestNew(t *testing.T) {
	kubeClient := fakekubeclientset.NewSimpleClientset()
	sharedClient := fakesharedclientset.NewSimpleClientset()
	servingClient := fakeclientset.NewSimpleClientset()
	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)

	serviceInformer := servingInformer.Serving().V1alpha1().Services()
	routeInformer := servingInformer.Serving().V1alpha1().Routes()
	configurationInformer := servingInformer.Serving().V1alpha1().Configurations()

	c := NewController(reconciler.Options{
		KubeClientSet:    kubeClient,
		SharedClientSet:  sharedClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}, serviceInformer, configurationInformer, routeInformer)

	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}

func svc(name, namespace string, so ...ServiceOption) *v1alpha1.Service {
	s := &v1alpha1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	for _, opt := range so {
		opt(s)
	}
	return s
}

func config(name, namespace string, so ServiceOption, co ...ConfigOption) *v1alpha1.Configuration {
	s := svc(name, namespace, so)
	cfg, err := resources.MakeConfiguration(s)
	if err != nil {
		panic(fmt.Sprintf("MakeConfiguration() = %v", err))
	}
	for _, opt := range co {
		opt(cfg)
	}
	return cfg
}

func route(name, namespace string, so ServiceOption, ro ...RouteOption) *v1alpha1.Route {
	s := svc(name, namespace, so)
	route, err := resources.MakeRoute(s)
	if err != nil {
		panic(fmt.Sprintf("MakeRoute() = %v", err))
	}
	for _, opt := range ro {
		opt(route)
	}
	return route
}

// TODO(mattmoor): Replace these when we refactor Route's table_test.go
func MutateRoute(rt *v1alpha1.Route) {
	rt.Spec = v1alpha1.RouteSpec{}
}

func RouteReady(cfg *v1alpha1.Route) {
	cfg.Status = v1alpha1.RouteStatus{
		Conditions: []duckv1alpha1.Condition{{
			Type:   "Ready",
			Status: "True",
		}},
	}
}

func RouteFailed(reason, message string) RouteOption {
	return func(cfg *v1alpha1.Route) {
		cfg.Status = v1alpha1.RouteStatus{
			Conditions: []duckv1alpha1.Condition{{
				Type:    "Ready",
				Status:  "False",
				Reason:  reason,
				Message: message,
			}},
		}
	}
}
