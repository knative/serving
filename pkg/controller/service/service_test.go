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
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/controller"

	. "github.com/knative/serving/pkg/controller/testing"
	. "github.com/knative/serving/pkg/logging/testing"
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
		Listers: Listers{
			Service: &ServiceLister{
				Items: []*v1alpha1.Service{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "incomplete",
						Namespace: "foo",
					},
					// There is no spec.{runLatest,pinned} in this
					// Service to trigger the error condition.
					Status: v1alpha1.ServiceStatus{
						Conditions: []v1alpha1.ServiceCondition{{
							Type:   v1alpha1.ServiceConditionReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionConfigurationsReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionRoutesReady,
							Status: corev1.ConditionUnknown,
						}},
					},
				}},
			},
		},
		Key:     "foo/incomplete",
		WantErr: true,
	}, {
		Name: "runLatest - create route and service",
		Listers: Listers{
			Service: &ServiceLister{
				Items: []*v1alpha1.Service{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "run-latest",
						Namespace: "foo",
					},
					Spec: v1alpha1.ServiceSpec{
						RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
					},
				}},
			},
		},
		Key: "foo/run-latest",
		WantCreates: []metav1.Object{
			&v1alpha1.Configuration{
				ObjectMeta: com("foo", "run-latest", or("run-latest")),
				Spec:       configSpec,
			},
			&v1alpha1.Route{
				ObjectMeta: com("foo", "run-latest", or("run-latest")),
				Spec:       runLatestSpec("run-latest"),
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "run-latest",
					Namespace: "foo",
				},
				Spec: v1alpha1.ServiceSpec{
					RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
				},
				Status: v1alpha1.ServiceStatus{
					Conditions: []v1alpha1.ServiceCondition{{
						Type:   v1alpha1.ServiceConditionReady,
						Status: corev1.ConditionUnknown,
					}, {
						Type:   v1alpha1.ServiceConditionConfigurationsReady,
						Status: corev1.ConditionUnknown,
					}, {
						Type:   v1alpha1.ServiceConditionRoutesReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
	}, {
		Name: "pinned - create route and service",
		Listers: Listers{
			Service: &ServiceLister{
				Items: []*v1alpha1.Service{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pinned",
						Namespace: "foo",
					},
					Spec: v1alpha1.ServiceSpec{
						Pinned: &v1alpha1.PinnedType{RevisionName: "pinned-0001", Configuration: configSpec},
					},
				}},
			},
		},
		Key: "foo/pinned",
		WantCreates: []metav1.Object{
			&v1alpha1.Configuration{
				ObjectMeta: com("foo", "pinned", or("pinned")),
				Spec:       configSpec,
			},
			&v1alpha1.Route{
				ObjectMeta: com("foo", "pinned", or("pinned")),
				Spec:       pinnedSpec("pinned-0001"),
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pinned",
					Namespace: "foo",
				},
				Spec: v1alpha1.ServiceSpec{
					Pinned: &v1alpha1.PinnedType{RevisionName: "pinned-0001", Configuration: configSpec},
				},
				Status: v1alpha1.ServiceStatus{
					Conditions: []v1alpha1.ServiceCondition{{
						Type:   v1alpha1.ServiceConditionReady,
						Status: corev1.ConditionUnknown,
					}, {
						Type:   v1alpha1.ServiceConditionConfigurationsReady,
						Status: corev1.ConditionUnknown,
					}, {
						Type:   v1alpha1.ServiceConditionRoutesReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
	}, {
		Name: "runLatest - no updates",
		Listers: Listers{
			Service: &ServiceLister{
				Items: []*v1alpha1.Service{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "no-updates",
						Namespace: "foo",
					},
					Spec: v1alpha1.ServiceSpec{
						RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
					},
					Status: v1alpha1.ServiceStatus{
						Conditions: []v1alpha1.ServiceCondition{{
							Type:   v1alpha1.ServiceConditionReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionConfigurationsReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionRoutesReady,
							Status: corev1.ConditionUnknown,
						}},
					},
				}},
			},
			Route: &RouteLister{
				Items: []*v1alpha1.Route{{
					ObjectMeta: om("foo", "no-updates"),
					Spec:       runLatestSpec("no-updates"),
				}},
			},
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "no-updates"),
					Spec:       configSpec,
				}},
			},
		},
		Key: "foo/no-updates",
	}, {
		Name: "runLatest - update route and service",
		Listers: Listers{
			Service: &ServiceLister{
				Items: []*v1alpha1.Service{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "update-route-and-config",
						Namespace: "foo",
					},
					Spec: v1alpha1.ServiceSpec{
						RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
					},
					Status: v1alpha1.ServiceStatus{
						Conditions: []v1alpha1.ServiceCondition{{
							Type:   v1alpha1.ServiceConditionReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionConfigurationsReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionRoutesReady,
							Status: corev1.ConditionUnknown,
						}},
					},
				}},
			},
			// Update the skeletal Config/Route to have the appropriate {Config,Route}Specs
			Route: &RouteLister{
				Items: []*v1alpha1.Route{{ObjectMeta: om("foo", "update-route-and-config")}},
			},
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{ObjectMeta: om("foo", "update-route-and-config")}},
			},
		},
		Key: "foo/update-route-and-config",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "update-route-and-config"),
				Spec:       configSpec,
			},
		}, {
			Object: &v1alpha1.Route{
				ObjectMeta: om("foo", "update-route-and-config"),
				Spec:       runLatestSpec("update-route-and-config"),
			},
		}},
	}, {
		Name: "runLatest - bad config update",
		Listers: Listers{
			Service: &ServiceLister{
				Items: []*v1alpha1.Service{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bad-config-update",
						Namespace: "foo",
					},
					Status: v1alpha1.ServiceStatus{
						Conditions: []v1alpha1.ServiceCondition{{
							Type:   v1alpha1.ServiceConditionReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionConfigurationsReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionRoutesReady,
							Status: corev1.ConditionUnknown,
						}},
					},
				}},
			},
			// Update the skeletal Config/Route to have the appropriate {Config,Route}Specs
			Route: &RouteLister{
				Items: []*v1alpha1.Route{{ObjectMeta: om("foo", "bad-config-update")}},
			},
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{ObjectMeta: om("foo", "bad-config-update")}},
			},
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
		Listers: Listers{
			Service: &ServiceLister{
				Items: []*v1alpha1.Service{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "create-route-failure",
						Namespace: "foo",
					},
					Spec: v1alpha1.ServiceSpec{
						RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
					},
				}},
			},
		},
		Key: "foo/create-route-failure",
		WantCreates: []metav1.Object{
			&v1alpha1.Configuration{
				ObjectMeta: com("foo", "create-route-failure", or("create-route-failure")),
				Spec:       configSpec,
			},
			&v1alpha1.Route{
				ObjectMeta: com("foo", "create-route-failure", or("create-route-failure")),
				Spec:       runLatestSpec("create-route-failure"),
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "create-route-failure",
					Namespace: "foo",
				},
				Spec: v1alpha1.ServiceSpec{
					RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
				},
				Status: v1alpha1.ServiceStatus{
					Conditions: []v1alpha1.ServiceCondition{{
						Type:   v1alpha1.ServiceConditionReady,
						Status: corev1.ConditionUnknown,
					}, {
						Type:   v1alpha1.ServiceConditionConfigurationsReady,
						Status: corev1.ConditionUnknown,
					}, {
						Type:   v1alpha1.ServiceConditionRoutesReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
	}, {
		Name: "runLatest - configuration creation failure",
		// Induce a failure during configuration creation
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "configurations"),
		},
		Listers: Listers{
			Service: &ServiceLister{
				Items: []*v1alpha1.Service{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "create-config-failure",
						Namespace: "foo",
					},
					Spec: v1alpha1.ServiceSpec{
						RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
					},
				}},
			},
		},
		Key: "foo/create-config-failure",
		WantCreates: []metav1.Object{
			&v1alpha1.Configuration{
				ObjectMeta: com("foo", "create-config-failure", or("create-config-failure")),
				Spec:       configSpec,
			},
			// We don't get to creating the Route.
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "create-config-failure",
					Namespace: "foo",
				},
				Spec: v1alpha1.ServiceSpec{
					RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
				},
				Status: v1alpha1.ServiceStatus{
					Conditions: []v1alpha1.ServiceCondition{{
						Type:   v1alpha1.ServiceConditionReady,
						Status: corev1.ConditionUnknown,
					}, {
						Type:   v1alpha1.ServiceConditionConfigurationsReady,
						Status: corev1.ConditionUnknown,
					}, {
						Type:   v1alpha1.ServiceConditionRoutesReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
	}, {
		Name: "runLatest - update route failure",
		// Induce a failure updating the route
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "routes"),
		},
		Listers: Listers{
			Service: &ServiceLister{
				Items: []*v1alpha1.Service{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "update-route-failure",
						Namespace: "foo",
					},
					Spec: v1alpha1.ServiceSpec{
						RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
					},
					Status: v1alpha1.ServiceStatus{
						Conditions: []v1alpha1.ServiceCondition{{
							Type:   v1alpha1.ServiceConditionReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionConfigurationsReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionRoutesReady,
							Status: corev1.ConditionUnknown,
						}},
					},
				}},
			},
			// Update the skeletal Config/Route to have the appropriate {Config,Route}Specs
			Route: &RouteLister{
				Items: []*v1alpha1.Route{{ObjectMeta: om("foo", "update-route-failure")}},
			},
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{ObjectMeta: om("foo", "update-route-failure")}},
			},
		},
		Key: "foo/update-route-failure",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "update-route-failure"),
				Spec:       configSpec,
			},
		}, {
			Object: &v1alpha1.Route{
				ObjectMeta: om("foo", "update-route-failure"),
				Spec:       runLatestSpec("update-route-failure"),
			},
		}},
	}, {
		Name: "runLatest - update config failure",
		// Induce a failure updating the config
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "configurations"),
		},
		Listers: Listers{
			Service: &ServiceLister{
				Items: []*v1alpha1.Service{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "update-config-failure",
						Namespace: "foo",
					},
					Spec: v1alpha1.ServiceSpec{
						RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
					},
					Status: v1alpha1.ServiceStatus{
						Conditions: []v1alpha1.ServiceCondition{{
							Type:   v1alpha1.ServiceConditionReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionConfigurationsReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionRoutesReady,
							Status: corev1.ConditionUnknown,
						}},
					},
				}},
			},
			// Update the skeletal Config/Route to have the appropriate {Config,Route}Specs
			Route: &RouteLister{
				Items: []*v1alpha1.Route{{ObjectMeta: om("foo", "update-config-failure")}},
			},
			Configuration: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{ObjectMeta: om("foo", "update-config-failure")}},
			},
		},
		Key: "foo/update-config-failure",
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "update-config-failure"),
				Spec:       configSpec,
			},
			// We don't get to updating the Route.
		}},
	}, {
		Name: "runLatest - failure updating service status",
		// Induce a failure updating the service status.
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "services"),
		},
		Listers: Listers{
			Service: &ServiceLister{
				Items: []*v1alpha1.Service{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "run-latest",
						Namespace: "foo",
					},
					Spec: v1alpha1.ServiceSpec{
						RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
					},
				}},
			},
		},
		Key: "foo/run-latest",
		WantCreates: []metav1.Object{
			&v1alpha1.Configuration{
				ObjectMeta: com("foo", "run-latest", or("run-latest")),
				Spec:       configSpec,
			},
			&v1alpha1.Route{
				ObjectMeta: com("foo", "run-latest", or("run-latest")),
				Spec:       runLatestSpec("run-latest"),
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "run-latest",
					Namespace: "foo",
				},
				Spec: v1alpha1.ServiceSpec{
					RunLatest: &v1alpha1.RunLatestType{Configuration: configSpec},
				},
				Status: v1alpha1.ServiceStatus{
					Conditions: []v1alpha1.ServiceCondition{{
						Type:   v1alpha1.ServiceConditionReady,
						Status: corev1.ConditionUnknown,
					}, {
						Type:   v1alpha1.ServiceConditionConfigurationsReady,
						Status: corev1.ConditionUnknown,
					}, {
						Type:   v1alpha1.ServiceConditionRoutesReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
	}}

	table.Test(t, func(listers *Listers, opt controller.Options) controller.Interface {
		return &Controller{
			Base:                controller.NewBase(opt, controllerAgentName, "Services"),
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

	c := NewController(controller.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}, serviceInformer, configurationInformer, routeInformer)

	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}

func runLatestSpec(name string) v1alpha1.RouteSpec {
	return v1alpha1.RouteSpec{
		Traffic: []v1alpha1.TrafficTarget{{
			ConfigurationName: name,
			Percent:           100,
		}},
	}
}

func pinnedSpec(name string) v1alpha1.RouteSpec {
	return v1alpha1.RouteSpec{
		Traffic: []v1alpha1.TrafficTarget{{
			RevisionName: name,
			Percent:      100,
		}},
	}
}

// or builds OwnerReferences for a child of a Service
func or(name string) []metav1.OwnerReference {
	return []metav1.OwnerReference{{
		APIVersion:         v1alpha1.SchemeGroupVersion.String(),
		Kind:               "Service",
		Name:               name,
		Controller:         &boolTrue,
		BlockOwnerDeletion: &boolTrue,
	}}
}

// om builds ObjectMeta for a Service
func om(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
}

// com builds the ObjectMeta for a Child of a Service
func com(namespace, name string, or []metav1.OwnerReference) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:            name,
		Namespace:       namespace,
		Labels:          map[string]string{serving.ServiceLabelKey: name},
		OwnerReferences: or,
	}
}
