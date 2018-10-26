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
	"testing"

	corev1 "k8s.io/api/core/v1"
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

var (
	boolTrue   = true
	configSpec = v1alpha1.ConfigurationSpec{
		RevisionTemplate: v1alpha1.RevisionTemplateSpec{
			Spec: v1alpha1.RevisionSpec{
				Container: corev1.Container{
					Image: "busybox",
				},
			},
		},
	}

	manualConditions = duckv1alpha1.Conditions{{
		Type:    v1alpha1.ServiceConditionConfigurationsReady,
		Status:  corev1.ConditionUnknown,
		Reason:  "Manual",
		Message: "Service is set to Manual, and is not managing underlying resources.",
	}, {
		Type:    v1alpha1.ServiceConditionReady,
		Status:  corev1.ConditionUnknown,
		Reason:  "Manual",
		Message: "Service is set to Manual, and is not managing underlying resources.",
	}, {
		Type:    v1alpha1.ServiceConditionRoutesReady,
		Status:  corev1.ConditionUnknown,
		Reason:  "Manual",
		Message: "Service is set to Manual, and is not managing underlying resources.",
	}}

	initialConditions = duckv1alpha1.Conditions{{
		Type:   v1alpha1.ServiceConditionConfigurationsReady,
		Status: corev1.ConditionUnknown,
	}, {
		Type:   v1alpha1.ServiceConditionReady,
		Status: corev1.ConditionUnknown,
	}, {
		Type:   v1alpha1.ServiceConditionRoutesReady,
		Status: corev1.ConditionUnknown,
	}}
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
			// There is no spec.{runLatest,pinned} in this Service to trigger the error condition.
			svc("incomplete", "foo", v1alpha1.ServiceSpec{}, initialConditions...),
		},
		Key:     "foo/incomplete",
		WantErr: true,
	}, {
		Name: "runLatest - create route and service",
		Objects: []runtime.Object{
			svcRL("run-latest", "foo"),
		},
		Key: "foo/run-latest",
		WantCreates: []metav1.Object{
			mustMakeConfig(t, svcRL("run-latest", "foo")),
			mustMakeRoute(t, svcRL("run-latest", "foo")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcRL("run-latest", "foo", initialConditions...),
		}},
	}, {
		Name: "pinned - create route and service",
		Objects: []runtime.Object{
			svcPin("pinned", "foo"),
		},
		Key: "foo/pinned",
		WantCreates: []metav1.Object{
			mustMakeConfig(t, svcPin("pinned", "foo")),
			mustMakeRoute(t, svcPin("pinned", "foo")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcPin("pinned", "foo", initialConditions...),
		}},
	}, {
		Name: "release - create route and service",
		Objects: []runtime.Object{
			svcRelease("release", "foo"),
		},
		Key: "foo/release",
		WantCreates: []metav1.Object{
			mustMakeConfig(t, svcRelease("release", "foo")),
			mustMakeRoute(t, svcRelease("release", "foo")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcRelease("release", "foo", initialConditions...),
		}},
	}, {
		Name: "manual- no creates",
		Objects: []runtime.Object{
			svcManual("manual", "foo"),
		},
		Key: "foo/manual",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcManual("manual", "foo", manualConditions...),
		}},
	}, {
		Name: "runLatest - no updates",
		Objects: []runtime.Object{
			svcRL("no-updates", "foo", initialConditions...),
			mustMakeRoute(t, svcRL("no-updates", "foo", initialConditions...)),
			mustMakeConfig(t, svcRL("no-updates", "foo", initialConditions...)),
		},
		Key: "foo/no-updates",
	}, {
		Name: "runLatest - update route and service",
		Objects: []runtime.Object{
			svcRL("update-route-and-config", "foo", initialConditions...),
			// Update the skeletal Config/Route to have the appropriate {Config,Route}Specs
			mutateConfig(mustMakeConfig(t, svcRL("update-route-and-config", "foo", initialConditions...))),
			mutateRoute(mustMakeRoute(t, svcRL("update-route-and-config", "foo", initialConditions...))),
		},
		Key: "foo/update-route-and-config",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: mustMakeConfig(t, svcRL("update-route-and-config", "foo", initialConditions...)),
		}, {
			Object: mustMakeRoute(t, svcRL("update-route-and-config", "foo", initialConditions...)),
		}},
	}, {
		Name: "runLatest - bad config update",
		Objects: []runtime.Object{
			// There is no spec.{runLatest,pinned} in this Service, which triggers the error
			// path updating Configuration.
			svc("bad-config-update", "foo", v1alpha1.ServiceSpec{}, initialConditions...),
			// Update the skeletal Config/Route to have the appropriate {Config,Route}Specs
			mutateConfig(mustMakeConfig(t, svcRL("bad-config-update", "foo", initialConditions...))),
			mutateRoute(mustMakeRoute(t, svcRL("bad-config-update", "foo", initialConditions...))),
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
			svcRL("create-route-failure", "foo"),
		},
		Key: "foo/create-route-failure",
		WantCreates: []metav1.Object{
			mustMakeConfig(t, svcRL("create-route-failure", "foo")),
			mustMakeRoute(t, svcRL("create-route-failure", "foo")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcRL("create-route-failure", "foo", initialConditions...),
		}},
	}, {
		Name: "runLatest - configuration creation failure",
		// Induce a failure during configuration creation
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "configurations"),
		},
		Objects: []runtime.Object{
			svcRL("create-config-failure", "foo"),
		},
		Key: "foo/create-config-failure",
		WantCreates: []metav1.Object{
			mustMakeConfig(t, svcRL("create-config-failure", "foo")),
			// We don't get to creating the Route.
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcRL("create-config-failure", "foo", initialConditions...),
		}},
	}, {
		Name: "runLatest - update route failure",
		// Induce a failure updating the route
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "routes"),
		},
		Objects: []runtime.Object{
			svcRL("update-route-failure", "foo", initialConditions...),
			// Update the skeletal Config/Route to have the appropriate {Config,Route}Specs
			mutateRoute(mustMakeRoute(t, svcRL("update-route-failure", "foo", initialConditions...))),
			mutateConfig(mustMakeConfig(t, svcRL("update-route-failure", "foo", initialConditions...))),
		},
		Key: "foo/update-route-failure",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: mustMakeConfig(t, svcRL("update-route-failure", "foo", initialConditions...)),
		}, {
			Object: mustMakeRoute(t, svcRL("update-route-failure", "foo", initialConditions...)),
		}},
	}, {
		Name: "runLatest - update config failure",
		// Induce a failure updating the config
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "configurations"),
		},
		Objects: []runtime.Object{
			svcRL("update-config-failure", "foo", initialConditions...),
			// Update the skeletal Config/Route to have the appropriate {Config,Route}Specs
			mutateRoute(mustMakeRoute(t, svcRL("update-config-failure", "foo", initialConditions...))),
			mutateConfig(mustMakeConfig(t, svcRL("update-config-failure", "foo", initialConditions...))),
		},
		Key: "foo/update-config-failure",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: mustMakeConfig(t, svcRL("update-config-failure", "foo", initialConditions...)),
			// We don't get to updating the Route.
		}},
	}, {
		Name: "runLatest - failure updating service status",
		// Induce a failure updating the service status.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "services"),
		},
		Objects: []runtime.Object{
			svcRL("run-latest", "foo"),
		},
		Key: "foo/run-latest",
		WantCreates: []metav1.Object{
			mustMakeConfig(t, svcRL("run-latest", "foo")),
			mustMakeRoute(t, svcRL("run-latest", "foo")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcRL("run-latest", "foo", initialConditions...),
		}},
	}, {
		Name: "runLatest - route and config ready, propagate ready",
		// When both route and config are ready, the service should become ready.
		Objects: []runtime.Object{
			svcRL("all-ready", "foo", initialConditions...),
			routeWithStatus(mustMakeRoute(t, svcRL("all-ready", "foo", initialConditions...)),
				v1alpha1.RouteStatus{
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.RouteConditionReady,
						Status: corev1.ConditionTrue,
					}},
				}),
			cfgWithStatus(mustMakeConfig(t, svcRL("all-ready", "foo", initialConditions...)),
				v1alpha1.ConfigurationStatus{
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					}},
				}),
		},
		Key: "foo/all-ready",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcRL("all-ready", "foo", duckv1alpha1.Conditions{{
				Type:   v1alpha1.ServiceConditionConfigurationsReady,
				Status: corev1.ConditionTrue,
			}, {
				Type:   v1alpha1.ServiceConditionReady,
				Status: corev1.ConditionTrue,
			}, {
				Type:   v1alpha1.ServiceConditionRoutesReady,
				Status: corev1.ConditionTrue,
			}}...),
		}},
	}, {
		Name: "runLatest - config fails, propagate failure",
		// When config fails, the service should fail.
		Objects: []runtime.Object{
			svcRL("config-fails", "foo", initialConditions...),
			routeWithStatus(mustMakeRoute(t, svcRL("config-fails", "foo", initialConditions...)),
				v1alpha1.RouteStatus{
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.RouteConditionReady,
						Status: corev1.ConditionTrue,
					}},
				}),
			cfgWithStatus(mustMakeConfig(t, svcRL("config-fails", "foo", initialConditions...)),
				v1alpha1.ConfigurationStatus{
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionFalse,
						Reason: "Propagate me, please",
					}},
				}),
		},
		Key: "foo/config-fails",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcRL("config-fails", "foo", duckv1alpha1.Conditions{{
				Type:   v1alpha1.ServiceConditionConfigurationsReady,
				Status: corev1.ConditionFalse,
				Reason: "Propagate me, please",
			}, {
				Type:   v1alpha1.ServiceConditionReady,
				Status: corev1.ConditionFalse,
				Reason: "Propagate me, please",
			}, {
				Type:   v1alpha1.ServiceConditionRoutesReady,
				Status: corev1.ConditionTrue,
			}}...),
		}},
	}, {
		Name: "runLatest - route fails, propagate failure",
		// When route fails, the service should fail.
		Objects: []runtime.Object{
			svcRL("route-fails", "foo", initialConditions...),
			routeWithStatus(mustMakeRoute(t, svcRL("route-fails", "foo", initialConditions...)),
				v1alpha1.RouteStatus{
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.RouteConditionReady,
						Status: corev1.ConditionFalse,
						Reason: "Propagate me, please",
					}},
				}),
			cfgWithStatus(mustMakeConfig(t, svcRL("route-fails", "foo", initialConditions...)),
				v1alpha1.ConfigurationStatus{
					Conditions: duckv1alpha1.Conditions{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					}},
				}),
		},
		Key: "foo/route-fails",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcRL("route-fails", "foo", duckv1alpha1.Conditions{{
				Type:   v1alpha1.ServiceConditionConfigurationsReady,
				Status: corev1.ConditionTrue,
			}, {
				Type:   v1alpha1.ServiceConditionReady,
				Status: corev1.ConditionFalse,
				Reason: "Propagate me, please",
			}, {
				Type:   v1alpha1.ServiceConditionRoutesReady,
				Status: corev1.ConditionFalse,
				Reason: "Propagate me, please",
			}}...),
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

func svc(name, namespace string, spec v1alpha1.ServiceSpec, conditions ...duckv1alpha1.Condition) *v1alpha1.Service {
	return &v1alpha1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
		Status: v1alpha1.ServiceStatus{
			Conditions: conditions,
		},
	}
}

func svcRL(name, namespace string, conditions ...duckv1alpha1.Condition) *v1alpha1.Service {
	return svc(name, namespace, v1alpha1.ServiceSpec{
		RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
	}, conditions...)
}

func svcPin(name, namespace string, conditions ...duckv1alpha1.Condition) *v1alpha1.Service {
	return svc(name, namespace, v1alpha1.ServiceSpec{
		Pinned: &v1alpha1.PinnedType{RevisionName: "pinned-0001", Configuration: configSpec},
	}, conditions...)
}

func svcRelease(name, namespace string, conditions ...duckv1alpha1.Condition) *v1alpha1.Service {
	return svc(name, namespace, v1alpha1.ServiceSpec{
		Release: &v1alpha1.ReleaseType{Revisions: []string{"release-00001", "release-00002"}, RolloutPercent: 49, Configuration: configSpec},
	}, conditions...)
}

func svcManual(name, namespace string, conditions ...duckv1alpha1.Condition) *v1alpha1.Service {
	return svc(name, namespace, v1alpha1.ServiceSpec{
		Manual: &v1alpha1.ManualType{},
	}, conditions...)
}

func mustMakeConfig(t *testing.T, svc *v1alpha1.Service) *v1alpha1.Configuration {
	cfg, err := resources.MakeConfiguration(svc)
	if err != nil {
		t.Fatalf("MakeConfiguration() = %v", err)
	}
	return cfg
}

func mustMakeRoute(t *testing.T, svc *v1alpha1.Service) *v1alpha1.Route {
	route, err := resources.MakeRoute(svc)
	if err != nil {
		t.Fatalf("MakeRoute() = %v", err)
	}
	return route
}

// mutateConfig mutates the specification of the Configuration to simulate someone editing it around our controller.
func mutateConfig(cfg *v1alpha1.Configuration) *v1alpha1.Configuration {
	cfg.Spec = v1alpha1.ConfigurationSpec{}
	return cfg
}

// mutateRoute mutates the specification of the Route to simulate someone editing it around our controller.
func mutateRoute(rt *v1alpha1.Route) *v1alpha1.Route {
	rt.Spec = v1alpha1.RouteSpec{}
	return rt
}

// TODO(#1762): Replace with builders.
func cfgWithStatus(cfg *v1alpha1.Configuration, s v1alpha1.ConfigurationStatus) *v1alpha1.Configuration {
	cfg.Status = s
	return cfg
}

// TODO(#1762): Replace with builders.
func routeWithStatus(rt *v1alpha1.Route, s v1alpha1.RouteStatus) *v1alpha1.Route {
	rt.Status = s
	return rt
}
