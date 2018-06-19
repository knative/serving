/*
Copyright 2018 Google LLC
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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/controller"

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
)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	type fields struct {
		s *ServiceLister
		r *RouteLister
		c *ConfigurationLister
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
		fields: fields{
			s: &ServiceLister{},
			r: &RouteLister{},
			c: &ConfigurationLister{},
		},
		key: "too/many/parts",
	}, {
		name: "key not found",
		fields: fields{
			s: &ServiceLister{},
			r: &RouteLister{},
			c: &ConfigurationLister{},
		},
		key: "foo/not-found",
	}, {
		name: "incomplete service",
		fields: fields{
			s: &ServiceLister{
				Items: []*v1alpha1.Service{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "incomplete",
						Namespace: "foo",
					},
					// There is no spec.{runLatest,pinned} in this
					// Service to trigger the error condition.
				}},
			},
			r: &RouteLister{},
			c: &ConfigurationLister{},
		},
		key:     "foo/incomplete",
		wantErr: true,
	}, {
		name: "runLatest - create route and service",
		fields: fields{
			s: &ServiceLister{
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
			r: &RouteLister{},
			c: &ConfigurationLister{},
		},
		key: "foo/run-latest",
		wantCreates: []metav1.Object{
			&v1alpha1.Configuration{
				ObjectMeta: om("foo", "run-latest"),
				Spec:       configSpec,
			},
			&v1alpha1.Route{
				ObjectMeta: om("foo", "run-latest"),
				Spec:       runLatestSpec("run-latest"),
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
						Type:   v1alpha1.ServiceConditionConfigurationReady,
						Status: corev1.ConditionUnknown,
					}, {
						Type:   v1alpha1.ServiceConditionRouteReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
	}, {
		name: "pinned - create route and service",
		fields: fields{
			s: &ServiceLister{
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
			r: &RouteLister{},
			c: &ConfigurationLister{},
		},
		key: "foo/pinned",
		wantCreates: []metav1.Object{
			&v1alpha1.Configuration{
				ObjectMeta: om("foo", "pinned"),
				Spec:       configSpec,
			},
			&v1alpha1.Route{
				ObjectMeta: om("foo", "pinned"),
				Spec:       pinnedSpec("pinned-0001"),
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
						Type:   v1alpha1.ServiceConditionConfigurationReady,
						Status: corev1.ConditionUnknown,
					}, {
						Type:   v1alpha1.ServiceConditionRouteReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
	}, {
		name: "runLatest - no updates",
		fields: fields{
			s: &ServiceLister{
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
							Type:   v1alpha1.ServiceConditionConfigurationReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionRouteReady,
							Status: corev1.ConditionUnknown,
						}},
					},
				}},
			},
			r: &RouteLister{
				Items: []*v1alpha1.Route{{
					ObjectMeta: om("foo", "no-updates"),
					Spec:       runLatestSpec("no-updates"),
				}},
			},
			c: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "no-updates"),
					Spec:       configSpec,
				}},
			},
		},
		key: "foo/no-updates",
	}, {
		name: "runLatest - update route and service",
		fields: fields{
			s: &ServiceLister{
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
							Type:   v1alpha1.ServiceConditionConfigurationReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionRouteReady,
							Status: corev1.ConditionUnknown,
						}},
					},
				}},
			},
			// Update the skeletal Config/Route to have the appropriate {Config,Route}Specs
			r: &RouteLister{
				Items: []*v1alpha1.Route{{ObjectMeta: om("foo", "update-route-and-config")}},
			},
			c: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{ObjectMeta: om("foo", "update-route-and-config")}},
			},
		},
		key: "foo/update-route-and-config",
		wantUpdates: []clientgotesting.UpdateActionImpl{{
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
		name: "runLatest - bad config update",
		fields: fields{
			s: &ServiceLister{
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
							Type:   v1alpha1.ServiceConditionConfigurationReady,
							Status: corev1.ConditionUnknown,
						}, {
							Type:   v1alpha1.ServiceConditionRouteReady,
							Status: corev1.ConditionUnknown,
						}},
					},
				}},
			},
			// Update the skeletal Config/Route to have the appropriate {Config,Route}Specs
			r: &RouteLister{
				Items: []*v1alpha1.Route{{ObjectMeta: om("foo", "bad-config-update")}},
			},
			c: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{ObjectMeta: om("foo", "bad-config-update")}},
			},
		},
		key:     "foo/bad-config-update",
		wantErr: true,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var names []string

			var objs []runtime.Object
			for _, s := range tt.fields.s.Items {
				objs = append(objs, s)
			}
			for _, c := range tt.fields.c.Items {
				objs = append(objs, c)
			}
			for _, r := range tt.fields.r.Items {
				objs = append(objs, r)
			}

			client := fakeclientset.NewSimpleClientset(objs...)
			c := &Controller{
				Base: controller.NewBase(controller.Options{
					KubeClientSet:    fakekubeclientset.NewSimpleClientset(),
					ServingClientSet: client,
					Logger:           zap.NewNop().Sugar(),
				}, controllerAgentName, "Services"),
				serviceLister:       tt.fields.s,
				configurationLister: tt.fields.c,
				routeLister:         tt.fields.r,
			}

			if err := c.Reconcile(tt.key); (err != nil) != tt.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tt.wantErr)
			}
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

			actions := client.Actions()

			for i := range tt.wantCreates {
				if i > len(actions)-1 {
					t.Fatalf("Reconcile() unexpected actions: %#v", client.Actions())
				}
				if actions[i].GetVerb() != "create" {
					t.Fatalf("Reconcile() unexpected actions: %#v", client.Actions())
				}
				action := actions[i].(clientgotesting.CreateAction)
				if action.GetNamespace() != expectedNamespace {
					t.Errorf("unexpected action[%d]: %#v", i, action)
				}
				obj := action.GetObject()
				if tt.wantCreates[i].GetName() == "<generated>" {
					tt.wantCreates[i].SetName(names[0])
					names = names[1:]
				}
				if diff := cmp.Diff(tt.wantCreates[i], obj, ignoreLastTransitionTime); diff != "" {
					t.Errorf("unexpected create (-want +got): %s", diff)
				}
			}
			actions = actions[len(tt.wantCreates):]

			for i := range tt.wantUpdates {
				if i > len(actions)-1 {
					t.Fatalf("Reconcile() unexpected actions: %#v", client.Actions())
				}
				if actions[i].GetVerb() != "update" {
					t.Fatalf("Reconcile() unexpected actions: %#v", client.Actions())
				}
				action := actions[i].(clientgotesting.UpdateAction)
				if diff := cmp.Diff(tt.wantUpdates[i].GetObject(), action.GetObject(), ignoreLastTransitionTime); diff != "" {
					t.Errorf("unexpected update (-want +got): %s", diff)
				}
			}
			actions = actions[len(tt.wantUpdates):]

			for i := range tt.wantDeletes {
				if i > len(actions)-1 {
					t.Fatalf("Reconcile() unexpected actions: %#v", client.Actions())
				}
				if actions[i].GetVerb() != "delete" {
					t.Fatalf("Reconcile() unexpected actions: %#v", client.Actions())
				}
				action := actions[i].(clientgotesting.DeleteAction)
				if action.GetName() != tt.wantDeletes[i].Name || action.GetNamespace() != expectedNamespace {
					t.Errorf("unexpected action[%d]: %#v", i, action)
				}
			}
			actions = actions[len(tt.wantDeletes):]

			if len(actions) != 0 {
				t.Fatalf("Reconcile() unexpected actions: %#v", actions)
			}
		})
	}
}

func TestNew(t *testing.T) {
	kubeClient := fakekubeclientset.NewSimpleClientset()
	servingClient := fakeclientset.NewSimpleClientset()
	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)

	serviceInformer := servingInformer.Serving().V1alpha1().Services()
	routeInformer := servingInformer.Serving().V1alpha1().Routes()
	configurationInformer := servingInformer.Serving().V1alpha1().Configurations()

	_ = NewController(controller.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           zap.NewNop().Sugar(),
	}, serviceInformer, configurationInformer, routeInformer, &rest.Config{})
}

var ignoreLastTransitionTime = cmp.FilterPath(func(p cmp.Path) bool {
	return strings.HasSuffix(p.String(), "LastTransitionTime.Time")
}, cmp.Ignore())

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

func or(name string) []metav1.OwnerReference {
	return []metav1.OwnerReference{{
		APIVersion:         v1alpha1.SchemeGroupVersion.String(),
		Kind:               "Service",
		Name:               name,
		Controller:         &boolTrue,
		BlockOwnerDeletion: &boolTrue,
	}}
}

func om(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:            name,
		Namespace:       namespace,
		Labels:          map[string]string{serving.ServiceLabelKey: name},
		OwnerReferences: or(name),
	}
}
