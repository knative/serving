/*
Copyright 2018 Google LLC.

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

package configuration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	fakebuildclientset "github.com/knative/build/pkg/client/clientset/versioned/fake"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	"github.com/knative/serving/pkg/controller"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	. "github.com/knative/serving/pkg/controller/testing"
)

var (
	boolTrue     = true
	revisionSpec = v1alpha1.RevisionSpec{
		Container: corev1.Container{
			Image: "busybox",
		},
	}
	buildSpec = buildv1alpha1.BuildSpec{
		Steps: []corev1.Container{{
			Image: "build-step1",
		}, {
			Image: "build-step2",
		}},
	}
)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	type fields struct {
		c *ConfigurationLister
		r *RevisionLister
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
			c: &ConfigurationLister{},
			r: &RevisionLister{},
		},
		key: "too/many/parts",
	}, {
		name: "key not found",
		fields: fields{
			c: &ConfigurationLister{},
			r: &RevisionLister{},
		},
		key: "foo/not-found",
	}, {
		name: "create revision matching generation",
		fields: fields{
			c: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "no-revisions-yet"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 1234,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: revisionSpec,
						},
					},
				}},
			},
			r: &RevisionLister{},
		},
		wantCreates: []metav1.Object{
			&v1alpha1.Revision{
				ObjectMeta: com("foo", "no-revisions-yet", 1234),
				Spec:       revisionSpec,
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "no-revisions-yet"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 1234,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "no-revisions-yet-01234",
					ObservedGeneration:        1234,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
		key: "foo/no-revisions-yet",
	}, {
		name: "create revision matching generation with build",
		fields: fields{
			c: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "need-rev-and-build"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 99998,
						Build:      &buildSpec,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: v1alpha1.RevisionSpec{
								Container: corev1.Container{
									Image: "busybox",
								},
							},
						},
					},
				}},
			},
			r: &RevisionLister{},
		},
		wantCreates: []metav1.Object{
			&buildv1alpha1.Build{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       "foo",
					Name:            "need-rev-and-build-99998",
					OwnerReferences: or("need-rev-and-build"),
				},
				Spec: buildSpec,
			},
			&v1alpha1.Revision{
				ObjectMeta: com("foo", "need-rev-and-build", 99998),
				Spec: v1alpha1.RevisionSpec{
					BuildName: "need-rev-and-build-99998",
					Container: corev1.Container{
						Image: "busybox",
					},
				},
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "need-rev-and-build"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 99998,
					Build:      &buildSpec,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "need-rev-and-build-99998",
					ObservedGeneration:        99998,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
		key: "foo/need-rev-and-build",
	}, {
		name: "reconcile revision matching generation (ready: unknown)",
		fields: fields{
			c: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "matching-revision-not-done"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 5432,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: revisionSpec,
						},
					},
				}},
			},
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{{
					ObjectMeta: com("foo", "matching-revision-not-done", 5432),
					Spec:       revisionSpec,
				}},
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "matching-revision-not-done"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 5432,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-not-done-05432",
					ObservedGeneration:        5432,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
		key: "foo/matching-revision-not-done",
	}, {
		name: "reconcile revision matching generation (ready: true)",
		fields: fields{
			c: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "matching-revision-done"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 5555,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: revisionSpec,
						},
					},
				}},
			},
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{{
					ObjectMeta: com("foo", "matching-revision-done", 5555),
					Spec:       revisionSpec,
					Status: v1alpha1.RevisionStatus{
						Conditions: []v1alpha1.RevisionCondition{{
							Type:   v1alpha1.RevisionConditionReady,
							Status: corev1.ConditionTrue,
						}},
					},
				}},
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "matching-revision-done"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 5555,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-done-05555",
					LatestReadyRevisionName:   "matching-revision-done-05555",
					ObservedGeneration:        5555,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionTrue,
					}},
				},
			},
		}},
		key: "foo/matching-revision-done",
	}, {
		name: "reconcile revision matching generation (ready: true, idempotent)",
		fields: fields{
			c: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "matching-revision-done-idempotent"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 5566,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: revisionSpec,
						},
					},
					Status: v1alpha1.ConfigurationStatus{
						LatestCreatedRevisionName: "matching-revision-done-idempotent-05566",
						LatestReadyRevisionName:   "matching-revision-done-idempotent-05566",
						ObservedGeneration:        5566,
						Conditions: []v1alpha1.ConfigurationCondition{{
							Type:   v1alpha1.ConfigurationConditionReady,
							Status: corev1.ConditionTrue,
						}},
					},
				}},
			},
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{{
					ObjectMeta: com("foo", "matching-revision-done-idempotent", 5566),
					Spec:       revisionSpec,
					Status: v1alpha1.RevisionStatus{
						Conditions: []v1alpha1.RevisionCondition{{
							Type:   v1alpha1.RevisionConditionReady,
							Status: corev1.ConditionTrue,
						}},
					},
				}},
			},
		},
		key: "foo/matching-revision-done-idempotent",
	}, {
		name: "reconcile revision matching generation (ready: false)",
		fields: fields{
			c: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "matching-revision-failed"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 5555,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: revisionSpec,
						},
					},
				}},
			},
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{{
					ObjectMeta: com("foo", "matching-revision-failed", 5555),
					Spec:       revisionSpec,
					Status: v1alpha1.RevisionStatus{
						Conditions: []v1alpha1.RevisionCondition{{
							Type:    v1alpha1.RevisionConditionReady,
							Status:  corev1.ConditionFalse,
							Reason:  "Armageddon",
							Message: "It's the end of the world as we know it.",
						}},
					},
				}},
			},
		},
		wantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "matching-revision-failed"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 5555,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "matching-revision-failed-05555",
					ObservedGeneration:        5555,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:    v1alpha1.ConfigurationConditionReady,
						Status:  corev1.ConditionFalse,
						Reason:  "RevisionFailed",
						Message: `revision "matching-revision-failed-05555" failed with message: It's the end of the world as we know it.`,
					}},
				},
			},
		}},
		key: "foo/matching-revision-failed",
	}, {
		name: "reconcile revision matching generation (ready: bad)",
		fields: fields{
			c: &ConfigurationLister{
				Items: []*v1alpha1.Configuration{{
					ObjectMeta: om("foo", "bad-condition"),
					Spec: v1alpha1.ConfigurationSpec{
						Generation: 5555,
						RevisionTemplate: v1alpha1.RevisionTemplateSpec{
							Spec: revisionSpec,
						},
					},
				}},
			},
			r: &RevisionLister{
				Items: []*v1alpha1.Revision{{
					ObjectMeta: com("foo", "bad-condition", 5555),
					Spec:       revisionSpec,
					Status: v1alpha1.RevisionStatus{
						Conditions: []v1alpha1.RevisionCondition{{
							Type:   v1alpha1.RevisionConditionReady,
							Status: "Bad",
						}},
					},
				}},
			},
		},
		wantErr: true,
		wantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: &v1alpha1.Configuration{
				ObjectMeta: om("foo", "bad-condition"),
				Spec: v1alpha1.ConfigurationSpec{
					Generation: 5555,
					RevisionTemplate: v1alpha1.RevisionTemplateSpec{
						Spec: revisionSpec,
					},
				},
				Status: v1alpha1.ConfigurationStatus{
					LatestCreatedRevisionName: "bad-condition-05555",
					ObservedGeneration:        5555,
					Conditions: []v1alpha1.ConfigurationCondition{{
						Type:   v1alpha1.ConfigurationConditionReady,
						Status: corev1.ConditionUnknown,
					}},
				},
			},
		}},
		key: "foo/bad-condition",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []runtime.Object
			for _, c := range tt.fields.c.Items {
				objs = append(objs, c)
			}
			for _, r := range tt.fields.r.Items {
				objs = append(objs, r)
			}

			kubeClient := fakekubeclientset.NewSimpleClientset()
			client := fakeclientset.NewSimpleClientset(objs...)
			buildClient := fakebuildclientset.NewSimpleClientset()
			c := &Controller{
				Base: controller.NewBase(controller.Options{
					KubeClientSet:    kubeClient,
					BuildClientSet:   buildClient,
					ServingClientSet: client,
					Logger:           zap.NewNop().Sugar(),
				}, controllerAgentName, "Configurations"),
				configurationLister: tt.fields.c,
				revisionLister:      tt.fields.r,
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
						t.Errorf("Unexpected verb %v", action.GetVerb())
					}
				}
			}

			for i, want := range tt.wantCreates {
				if i >= len(createActions) {
					t.Errorf("Missing create: %v", want)
					continue
				}
				got := createActions[i]
				if got.GetNamespace() != expectedNamespace {
					t.Errorf("unexpected action[%d]: %#v", i, got)
				}
				obj := got.GetObject()
				if diff := cmp.Diff(want, obj, ignoreLastTransitionTime); diff != "" {
					t.Errorf("unexpected create (-want +got): %s", diff)
				}
			}
			if got, want := len(createActions), len(tt.wantCreates); got > want {
				t.Errorf("Extra creates: %v", createActions[want:])
			}

			for i, want := range tt.wantUpdates {
				if i >= len(updateActions) {
					t.Errorf("Missing update: %v", want)
					continue
				}
				got := updateActions[i]
				if diff := cmp.Diff(want.GetObject(), got.GetObject(), ignoreLastTransitionTime); diff != "" {
					t.Errorf("unexpected update (-want +got): %s", diff)
				}
			}
			if got, want := len(updateActions), len(tt.wantUpdates); got > want {
				t.Errorf("Extra updates: %v", updateActions[want:])
			}

			for i, want := range tt.wantDeletes {
				if i >= len(deleteActions) {
					t.Errorf("Missing delete: %v", want)
					continue
				}
				got := deleteActions[i]
				if got.GetName() != want.Name || got.GetNamespace() != expectedNamespace {
					t.Errorf("unexpected delete[%d]: %#v", i, got)
				}
			}
			if got, want := len(deleteActions), len(tt.wantDeletes); got > want {
				t.Errorf("Extra deletes: %v", deleteActions[want:])
			}
		})
	}
}

var ignoreLastTransitionTime = cmp.FilterPath(func(p cmp.Path) bool {
	return strings.HasSuffix(p.String(), "LastTransitionTime.Time")
}, cmp.Ignore())

func or(name string) []metav1.OwnerReference {
	return []metav1.OwnerReference{{
		APIVersion:         v1alpha1.SchemeGroupVersion.String(),
		Kind:               "Configuration",
		Name:               name,
		Controller:         &boolTrue,
		BlockOwnerDeletion: &boolTrue,
	}}
}

func om(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
}

func com(namespace, name string, generation int) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      fmt.Sprintf("%s-%05d", name, generation),
		Namespace: namespace,
		Annotations: map[string]string{
			serving.ConfigurationGenerationAnnotationKey: fmt.Sprintf("%v", generation),
		},
		Labels: map[string]string{
			serving.ConfigurationLabelKey: name,
		},
		OwnerReferences: or(name),
	}
}
