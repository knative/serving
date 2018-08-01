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

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/service/resources"

	. "github.com/knative/pkg/logging/testing"
	. "github.com/knative/serving/pkg/controller/testing"
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

	initialConditions = []v1alpha1.ServiceCondition{{
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
			resources.MakeRoute(svcRL("run-latest", "foo")),
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
			resources.MakeRoute(svcPin("pinned", "foo")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcPin("pinned", "foo", initialConditions...),
		}},
	}, {
		Name: "runLatest - no updates",
		Objects: []runtime.Object{
			svcRL("no-updates", "foo", initialConditions...),
			resources.MakeRoute(svcRL("no-updates", "foo", initialConditions...)),
			mustMakeConfig(t, svcRL("no-updates", "foo", initialConditions...)),
		},
		Key: "foo/no-updates",
	}, {
		Name: "runLatest - update route and service",
		Objects: []runtime.Object{
			svcRL("update-route-and-config", "foo", initialConditions...),
			// Update the skeletal Config/Route to have the appropriate {Config,Route}Specs
			mutateConfig(mustMakeConfig(t, svcRL("update-route-and-config", "foo", initialConditions...))),
			mutateRoute(resources.MakeRoute(svcRL("update-route-and-config", "foo", initialConditions...))),
		},
		Key: "foo/update-route-and-config",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: mustMakeConfig(t, svcRL("update-route-and-config", "foo", initialConditions...)),
		}, {
			Object: resources.MakeRoute(svcRL("update-route-and-config", "foo", initialConditions...)),
		}},
	}, {
		Name: "runLatest - bad config update",
		Objects: []runtime.Object{
			// There is no spec.{runLatest,pinned} in this Service, which triggers the error
			// path updating Configuration.
			svc("bad-config-update", "foo", v1alpha1.ServiceSpec{}, initialConditions...),
			// Update the skeletal Config/Route to have the appropriate {Config,Route}Specs
			mutateConfig(mustMakeConfig(t, svcRL("bad-config-update", "foo", initialConditions...))),
			mutateRoute(resources.MakeRoute(svcRL("bad-config-update", "foo", initialConditions...))),
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
			resources.MakeRoute(svcRL("create-route-failure", "foo")),
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
			mutateRoute(resources.MakeRoute(svcRL("update-route-failure", "foo", initialConditions...))),
			mutateConfig(mustMakeConfig(t, svcRL("update-route-failure", "foo", initialConditions...))),
		},
		Key: "foo/update-route-failure",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: mustMakeConfig(t, svcRL("update-route-failure", "foo", initialConditions...)),
		}, {
			Object: resources.MakeRoute(svcRL("update-route-failure", "foo", initialConditions...)),
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
			mutateRoute(resources.MakeRoute(svcRL("update-config-failure", "foo", initialConditions...))),
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
			resources.MakeRoute(svcRL("run-latest", "foo")),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcRL("run-latest", "foo", initialConditions...),
		}},
	}}

	table.Test(t, func(listers *Listers, opt controller.ReconcileOptions) controller.Reconciler {
		return &Reconciler{
			Base:                controller.NewBase(opt, controllerAgentName),
			serviceLister:       listers.GetServiceLister(),
			configurationLister: listers.GetConfigurationLister(),
			routeLister:         listers.GetRouteLister(),
		}
	})
}

func TestNew(t *testing.T) {
	kubeClient := fakekubeclientset.NewSimpleClientset()
	servingClient := fakeclientset.NewSimpleClientset()
	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)

	serviceInformer := servingInformer.Serving().V1alpha1().Services()
	routeInformer := servingInformer.Serving().V1alpha1().Routes()
	configurationInformer := servingInformer.Serving().V1alpha1().Configurations()

	c := NewController(controller.ReconcileOptions{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}, serviceInformer, configurationInformer, routeInformer)

	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}

func svc(name, namespace string, spec v1alpha1.ServiceSpec, conditions ...v1alpha1.ServiceCondition) *v1alpha1.Service {
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

func svcRL(name, namespace string, conditions ...v1alpha1.ServiceCondition) *v1alpha1.Service {
	return svc(name, namespace, v1alpha1.ServiceSpec{
		RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
	}, conditions...)
}

func svcPin(name, namespace string, conditions ...v1alpha1.ServiceCondition) *v1alpha1.Service {
	return svc(name, namespace, v1alpha1.ServiceSpec{
		Pinned: &v1alpha1.PinnedType{RevisionName: "pinned-0001", Configuration: configSpec},
	}, conditions...)
}

func mustMakeConfig(t *testing.T, svc *v1alpha1.Service) *v1alpha1.Configuration {
	cfg, err := resources.MakeConfiguration(svc)
	if err != nil {
		t.Fatalf("MakeConfiguration() = %v", err)
	}
	return cfg
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
