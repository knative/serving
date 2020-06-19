/*
Copyright 2019 The Knative Authors.

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

package gc

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgotesting "k8s.io/client-go/testing"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	pkgrec "knative.dev/pkg/reconciler"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	servingclient "knative.dev/serving/pkg/client/injection/client/fake"
	configreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/configuration"
	gcconfig "knative.dev/serving/pkg/gc"
	"knative.dev/serving/pkg/reconciler/configuration/resources"
	"knative.dev/serving/pkg/reconciler/gc/config"

	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing/v1"
)

var revisionSpec = v1.RevisionSpec{
	PodSpec: corev1.PodSpec{
		Containers: []corev1.Container{{
			Image: "busybox",
		}},
	},
	TimeoutSeconds: ptr.Int64(60),
}

func TestGCReconcile(t *testing.T) {
	now := time.Now()
	tenMinutesAgo := now.Add(-10 * time.Minute)

	old := now.Add(-11 * time.Minute)
	older := now.Add(-12 * time.Minute)
	oldest := now.Add(-13 * time.Minute)

	table := TableTest{{
		Name: "delete oldest, keep two",
		Objects: []runtime.Object{
			cfg("keep-two", "foo", 5556,
				WithLatestCreated("5556"),
				WithLatestReady("5556"),
				WithConfigObservedGen),
			rev("keep-two", "foo", 5554, MarkRevisionReady,
				WithRevName("5554"),
				WithCreationTimestamp(oldest),
				WithLastPinned(tenMinutesAgo)),
			rev("keep-two", "foo", 5555, MarkRevisionReady,
				WithRevName("5555"),
				WithCreationTimestamp(older),
				WithLastPinned(tenMinutesAgo)),
			rev("keep-two", "foo", 5556, MarkRevisionReady,
				WithRevName("5556"),
				WithCreationTimestamp(old),
				WithLastPinned(tenMinutesAgo)),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "foo",
				Verb:      "delete",
				Resource: schema.GroupVersionResource{
					Group:    "serving.knative.dev",
					Version:  "v1",
					Resource: "revisions",
				},
			},
			Name: "5554",
		}},
		Key: "foo/keep-two",
	}, {
		Name: "keep oldest when no lastPinned",
		Objects: []runtime.Object{
			cfg("keep-no-last-pinned", "foo", 5556,
				WithLatestCreated("5556"),
				WithLatestReady("5556"),
				WithConfigObservedGen),
			// No lastPinned so we will keep this.
			rev("keep-no-last-pinned", "foo", 5554, MarkRevisionReady,
				WithRevName("5554"),
				WithCreationTimestamp(oldest)),
			rev("keep-no-last-pinned", "foo", 5555, MarkRevisionReady,
				WithRevName("5555"),
				WithCreationTimestamp(older),
				WithLastPinned(tenMinutesAgo)),
			rev("keep-no-last-pinned", "foo", 5556, MarkRevisionReady,
				WithRevName("5556"),
				WithCreationTimestamp(old),
				WithLastPinned(tenMinutesAgo)),
		},
		Key: "foo/keep-no-last-pinned",
	}, {
		Name: "keep recent lastPinned",
		Objects: []runtime.Object{
			cfg("keep-recent-last-pinned", "foo", 5556,
				WithLatestCreated("5556"),
				WithLatestReady("5556"),
				WithConfigObservedGen),
			rev("keep-recent-last-pinned", "foo", 5554, MarkRevisionReady,
				WithRevName("5554"),
				WithCreationTimestamp(oldest),
				// This is an indication that things are still routing here.
				WithLastPinned(now)),
			rev("keep-recent-last-pinned", "foo", 5555, MarkRevisionReady,
				WithRevName("5555"),
				WithCreationTimestamp(older),
				WithLastPinned(tenMinutesAgo)),
			rev("keep-recent-last-pinned", "foo", 5556, MarkRevisionReady,
				WithRevName("5556"),
				WithCreationTimestamp(old),
				WithLastPinned(tenMinutesAgo)),
		},
		Key: "foo/keep-recent-last-pinned",
	}, {
		Name: "keep LatestReadyRevision",
		Objects: []runtime.Object{
			// Create a revision where the LatestReady is 5554, but LatestCreated is 5556.
			// We should keep LatestReady even if it is old.
			cfg("keep-two", "foo", 5556,
				WithLatestReady("5554"),
				// This comes after 'WithLatestReady' so the
				// Configuration's 'Ready' Status is 'Unknown'
				WithLatestCreated("5556"),
				WithConfigObservedGen),
			rev("keep-two", "foo", 5554, MarkRevisionReady,
				WithRevName("5554"),
				WithCreationTimestamp(oldest),
				WithLastPinned(tenMinutesAgo)),
			rev("keep-two", "foo", 5555, // Not Ready
				WithRevName("5555"),
				WithCreationTimestamp(older),
				WithLastPinned(tenMinutesAgo)),
			rev("keep-two", "foo", 5556, // Not Ready
				WithRevName("5556"),
				WithCreationTimestamp(old),
				WithLastPinned(tenMinutesAgo)),
		},
		Key: "foo/keep-two",
	}, {
		Name: "keep stale revision because of minimum generations",
		Objects: []runtime.Object{
			cfg("keep-all", "foo", 5554,
				// Don't set the latest ready revision here
				// since those by default are always retained
				WithLatestCreated("keep-all"),
				WithConfigObservedGen),
			rev("keep-all", "foo", 5554,
				WithRevName("keep-all"),
				WithCreationTimestamp(oldest),
				WithLastPinned(tenMinutesAgo)),
		},
		Key: "foo/keep-all",
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		r := &reconciler{
			client:         servingclient.Get(ctx),
			revisionLister: listers.GetRevisionLister(),
		}
		return configreconciler.NewReconciler(ctx, logging.FromContext(ctx),
			servingclient.Get(ctx), listers.GetConfigurationLister(),
			controller.GetEventRecorder(ctx), r, controller.Options{
				ConfigStore: &testConfigStore{
					config: &config.Config{
						RevisionGC: &gcconfig.Config{
							StaleRevisionCreateDelay:        5 * time.Minute,
							StaleRevisionTimeout:            5 * time.Minute,
							StaleRevisionMinimumGenerations: 2,
						},
					},
				}})
	}))
}

func TestIsRevisionStale(t *testing.T) {
	curTime := time.Now()
	staleTime := curTime.Add(-10 * time.Minute)

	tests := []struct {
		name      string
		rev       *v1.Revision
		latestRev string
		want      bool
	}{{
		name: "fresh revision that was never pinned",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(curTime),
			},
		},
		want: false,
	}, {
		name: "stale revision that was never pinned w/ Ready status",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
			},
			Status: v1.RevisionStatus{
				Status: duckv1.Status{
					Conditions: duckv1.Conditions{{
						Type:   v1.RevisionConditionReady,
						Status: "True",
					}},
				},
			},
		},
		want: false,
	}, {
		name: "stale revision that was never pinned w/o Ready status",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
			},
			Status: v1.RevisionStatus{
				Status: duckv1.Status{
					Conditions: duckv1.Conditions{{
						Type:   v1.RevisionConditionReady,
						Status: "Unknown",
					}},
				},
			},
		},
		want: true,
	}, {
		name: "stale revision that was previously pinned",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
				Annotations: map[string]string{
					"serving.knative.dev/lastPinned": fmt.Sprintf("%d", staleTime.Unix()),
				},
			},
		},
		want: true,
	}, {
		name: "fresh revision that was previously pinned",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
				Annotations: map[string]string{
					"serving.knative.dev/lastPinned": fmt.Sprintf("%d", curTime.Unix()),
				},
			},
		},
		want: false,
	}, {
		name: "stale latest ready revision",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
				Annotations: map[string]string{
					"serving.knative.dev/lastPinned": fmt.Sprintf("%d", staleTime.Unix()),
				},
			},
		},
		latestRev: "myrev",
		want:      false,
	}}

	cfgStore := testConfigStore{
		config: &config.Config{
			RevisionGC: &gcconfig.Config{
				StaleRevisionCreateDelay:        5 * time.Minute,
				StaleRevisionTimeout:            5 * time.Minute,
				StaleRevisionMinimumGenerations: 2,
			},
		},
	}
	ctx := cfgStore.ToContext(context.Background())

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &v1.Configuration{
				Status: v1.ConfigurationStatus{
					ConfigurationStatusFields: v1.ConfigurationStatusFields{
						LatestReadyRevisionName: test.latestRev,
					},
				},
			}

			got := isRevisionStale(ctx, test.rev, cfg)

			if got != test.want {
				t.Errorf("IsRevisionStale want %v got %v", test.want, got)
			}
		})
	}
}

func cfg(name, namespace string, generation int64, co ...ConfigOption) *v1.Configuration {
	c := &v1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: generation,
		},
		Spec: v1.ConfigurationSpec{
			Template: v1.RevisionTemplateSpec{
				Spec: *revisionSpec.DeepCopy(),
			},
		},
	}
	for _, opt := range co {
		opt(c)
	}
	c.SetDefaults(context.Background())
	return c
}

func rev(name, namespace string, generation int64, ro ...RevisionOption) *v1.Revision {
	config := cfg(name, namespace, generation)
	rev := resources.MakeRevision(config)
	rev.SetDefaults(context.Background())

	for _, opt := range ro {
		opt(rev)
	}
	return rev
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

var _ pkgrec.ConfigStore = (*testConfigStore)(nil)
