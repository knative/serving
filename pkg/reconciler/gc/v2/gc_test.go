/*
Copyright 2020 The Knative Authors

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

package v2

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	clientgotesting "k8s.io/client-go/testing"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/ptr"
	pkgrec "knative.dev/pkg/reconciler"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	"knative.dev/serving/pkg/gc"
	"knative.dev/serving/pkg/reconciler/configuration/resources"
	"knative.dev/serving/pkg/reconciler/gc/config"

	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration/fake"

	. "knative.dev/pkg/logging/testing"
	. "knative.dev/pkg/reconciler/testing"
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

func TestCollectMin(t *testing.T) {
	cfgMap := &config.Config{
		RevisionGC: &gc.Config{
			RetainSinceCreateTime:     5 * time.Minute,
			RetainSinceLastActiveTime: 5 * time.Minute,
			MinNonActiveRevisions:     1,
			MaxNonActiveRevisions:     -1, // assert no changes to min case
		},
	}

	now := time.Now()
	old := now.Add(-11 * time.Minute)
	older := now.Add(-12 * time.Minute)
	oldest := now.Add(-13 * time.Minute)
	fc := clock.NewFakePassiveClock(now)

	table := []struct {
		name        string
		cfg         *v1.Configuration
		revs        []*v1.Revision
		wantDeletes []clientgotesting.DeleteActionImpl
	}{{
		name: "too few revisions",
		cfg: cfg("none-reserved", "foo", 5556,
			WithLatestCreated("5556"),
			WithLatestReady("5556"),
			WithConfigObservedGen),
		revs: []*v1.Revision{
			rev("none-reserved", "foo", 5556, MarkRevisionReady,
				WithRevName("5556"),
				WithRoutingState(v1.RoutingStateActive, fc),
				WithCreationTimestamp(old)),
		},
	}, {
		name: "delete oldest, keep one recent, one active",
		cfg: cfg("keep-two", "foo", 5556,
			WithLatestCreated("5556"),
			WithLatestReady("5556"),
			WithConfigObservedGen),
		revs: []*v1.Revision{
			// Stale, oldest should be deleted
			rev("keep-two", "foo", 5554, MarkRevisionReady,
				WithRevName("5554"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(oldest)),
			// Stale, but MinNonActiveRevisions is 1
			rev("keep-two", "foo", 5555, MarkRevisionReady,
				WithRevName("5555"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(older)),
			// Actively referenced by Configuration
			rev("keep-two", "foo", 5556, MarkRevisionReady,
				WithRevName("5556"),
				WithRoutingState(v1.RoutingStateActive, fc),
				WithRoutingStateModified(old)),
		},
		wantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "foo",
				Verb:      "delete",
				Resource:  v1.SchemeGroupVersion.WithResource("revisions"),
			},
			Name: "5554",
		}},
	}, {
		name: "no latest ready, one active",
		cfg:  cfg("keep-two", "foo", 5556, WithConfigObservedGen),
		revs: []*v1.Revision{
			// Stale, oldest should be deleted
			rev("keep-two", "foo", 5554, MarkRevisionReady,
				WithRevName("5554"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(oldest)),
			// Stale, but MinNonActiveRevisions is 1
			rev("keep-two", "foo", 5555, MarkRevisionReady,
				WithRevName("5555"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(older)),
			// Actively referenced by Configuration
			rev("keep-two", "foo", 5556, MarkRevisionReady,
				WithRevName("5556"),
				WithRoutingState(v1.RoutingStateActive, fc),
				WithRoutingStateModified(old)),
		},
		wantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "foo",
				Verb:      "delete",
				Resource:  v1.SchemeGroupVersion.WithResource("revisions"),
			},
			Name: "5554",
		}},
	}, {
		name: "keep oldest when none Reserved",
		cfg: cfg("none-reserved", "foo", 5556,
			WithLatestCreated("5556"),
			WithLatestReady("5556"),
			WithConfigObservedGen),
		revs: []*v1.Revision{
			rev("none-reserved", "foo", 5554, MarkRevisionReady,
				WithRevName("5554"),
				WithRoutingState(v1.RoutingStatePending, fc),
				WithCreationTimestamp(oldest)),
			rev("none-reserved", "foo", 5555, MarkRevisionReady,
				WithRevName("5555"),
				WithRoutingState(v1.RoutingStateUnset, fc),
				WithCreationTimestamp(older)),
			rev("none-reserved", "foo", 5556, MarkRevisionReady,
				WithRevName("5556"),
				WithRoutingState(v1.RoutingStateActive, fc),
				WithCreationTimestamp(old)),
		},
	}, {
		name: "none stale",
		cfg:  cfg("none-stale", "foo", 5556, WithConfigObservedGen),
		revs: []*v1.Revision{
			rev("none-stale", "foo", 5554, MarkRevisionReady,
				WithRevName("5554"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(now)),
			rev("none-stale", "foo", 5555, MarkRevisionReady,
				WithRevName("5555"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(now)),
			rev("none-stale", "foo", 5556, MarkRevisionReady,
				WithRevName("5556"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(now)),
		},
	}, {
		name: "keep oldest because of the preserve annotation",
		cfg:  cfg("keep-oldest", "foo", 5556, WithConfigObservedGen),
		revs: []*v1.Revision{
			rev("keep-oldest", "foo", 5554, MarkRevisionReady,
				WithRevName("5554"),
				WithRoutingStateModified(oldest),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRevisionPreserveAnnotation()),
			rev("keep-oldest", "foo", 5555, MarkRevisionReady,
				WithRevName("5555"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(older)),
			rev("keep-oldest", "foo", 5556, MarkRevisionReady,
				WithRevName("5556"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(old)),
		},
		wantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "foo",
				Verb:      "delete",
				Resource:  v1.SchemeGroupVersion.WithResource("revisions"),
			},
			Name: "5555",
		}},
	}}

	for _, test := range table {
		t.Run(test.name, func(t *testing.T) {
			runTest(t, cfgMap, test.revs, test.cfg, test.wantDeletes)
		})
	}
}

func TestCollectMax(t *testing.T) {
	cfgMap := &config.Config{
		RevisionGC: &gc.Config{
			RetainSinceCreateTime:     1 * time.Hour,
			RetainSinceLastActiveTime: 1 * time.Hour,
			MinNonActiveRevisions:     1,
			MaxNonActiveRevisions:     2,
		},
	}

	now := time.Now()
	old := now.Add(-11 * time.Minute)
	older := now.Add(-12 * time.Minute)
	oldest := now.Add(-13 * time.Minute)
	fc := clock.NewFakePassiveClock(now)

	table := []struct {
		name        string
		cfg         *v1.Configuration
		revs        []*v1.Revision
		wantDeletes []clientgotesting.DeleteActionImpl
	}{{
		name: "at max",
		cfg: cfg("at max", "foo", 5556,
			WithLatestCreated("5556"),
			WithLatestReady("5556"),
			WithConfigObservedGen),
		revs: []*v1.Revision{
			// Under max
			rev("at max", "foo", 5554, MarkRevisionReady,
				WithRevName("5554"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(older)),
			// Under max
			rev("at max", "foo", 5555, MarkRevisionReady,
				WithRevName("5555"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(older)),
			// Actively referenced by Configuration
			rev("at max", "foo", 5556, MarkRevisionReady,
				WithRevName("5556"),
				WithRoutingState(v1.RoutingStateActive, fc),
				WithRoutingStateModified(old)),
		},
	}, {
		name: "delete oldest, keep three max",
		cfg: cfg("delete oldest", "foo", 5556,
			WithLatestCreated("5556"),
			WithLatestReady("5556"),
			WithConfigObservedGen),
		revs: []*v1.Revision{
			// Stale and over the max
			rev("delete oldest", "foo", 5553, MarkRevisionReady,
				WithRevName("5553"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(oldest)),
			// Stale but under max
			rev("delete oldest", "foo", 5554, MarkRevisionReady,
				WithRevName("5554"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(older)),
			// Stale but under max
			rev("delete oldest", "foo", 5555, MarkRevisionReady,
				WithRevName("5555"),
				WithRoutingState(v1.RoutingStateReserve, fc),
				WithRoutingStateModified(older)),
			// Actively referenced by Configuration
			rev("keep-two", "foo", 5556, MarkRevisionReady,
				WithRevName("5556"),
				WithRoutingState(v1.RoutingStateActive, fc),
				WithRoutingStateModified(old)),
		},
		wantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "foo",
				Verb:      "delete",
				Resource:  v1.SchemeGroupVersion.WithResource("revisions"),
			},
			Name: "5553",
		}},
	}, {
		name: "over max, all active",
		cfg: cfg("keep-two", "foo", 5556,
			WithLatestCreated("5556"),
			WithLatestReady("5556"),
			WithConfigObservedGen),
		revs: []*v1.Revision{
			rev("keep-two", "foo", 5553, MarkRevisionReady,
				WithRevName("5553"),
				WithRoutingState(v1.RoutingStateActive, fc)),
			rev("keep-two", "foo", 5554, MarkRevisionReady,
				WithRevName("5554"),
				WithRoutingState(v1.RoutingStateActive, fc)),
			rev("keep-two", "foo", 5555, MarkRevisionReady,
				WithRevName("5555"),
				WithRoutingState(v1.RoutingStateActive, fc)),
			rev("keep-two", "foo", 5556, MarkRevisionReady,
				WithRevName("5556"),
				WithRoutingState(v1.RoutingStateActive, fc)),
		},
	}}

	for _, test := range table {
		t.Run(test.name, func(t *testing.T) {
			runTest(t, cfgMap, test.revs, test.cfg, test.wantDeletes)
		})
	}
}

func TestCollectSettings(t *testing.T) {
	now := time.Now()
	old := now.Add(-11 * time.Minute)
	older := now.Add(-12 * time.Minute)
	oldest := now.Add(-13 * time.Minute)
	fc := clock.NewFakePassiveClock(now)

	cfg := cfg("settings-test", "foo", 5556,
		WithLatestCreated("5556"),
		WithLatestReady("5556"),
		WithConfigObservedGen)

	revs := []*v1.Revision{
		rev("settings-test", "foo", 5554, MarkRevisionReady,
			WithRevName("5554"),
			WithRoutingState(v1.RoutingStateReserve, fc),
			WithRoutingStateModified(oldest)),
		rev("settings-test", "foo", 5555, MarkRevisionReady,
			WithRevName("5555"),
			WithRoutingState(v1.RoutingStateReserve, fc),
			WithRoutingStateModified(older)),
		rev("settings-test", "foo", 5556, MarkRevisionReady,
			WithRevName("5556"),
			WithRoutingState(v1.RoutingStateActive, fc),
			WithRoutingStateModified(old)),
	}

	table := []struct {
		name        string
		gc          gc.Config
		wantDeletes []clientgotesting.DeleteActionImpl
	}{{
		name: "all disabled",
		gc: gc.Config{
			RetainSinceCreateTime:     time.Duration(gc.Disabled),
			RetainSinceLastActiveTime: time.Duration(gc.Disabled),
			MinNonActiveRevisions:     1,
			MaxNonActiveRevisions:     gc.Disabled,
		},
	}, {
		name: "staleness disabled",
		gc: gc.Config{
			RetainSinceCreateTime:     time.Duration(gc.Disabled),
			RetainSinceLastActiveTime: time.Duration(gc.Disabled),
			MinNonActiveRevisions:     0,
			MaxNonActiveRevisions:     1,
		},
		wantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "foo",
				Verb:      "delete",
				Resource:  v1.SchemeGroupVersion.WithResource("revisions"),
			},
			Name: "5554",
		}},
	}, {
		name: "max disabled",
		gc: gc.Config{
			RetainSinceCreateTime:     time.Duration(gc.Disabled),
			RetainSinceLastActiveTime: 1 * time.Minute,
			MinNonActiveRevisions:     1,
			MaxNonActiveRevisions:     gc.Disabled,
		},
		wantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "foo",
				Verb:      "delete",
				Resource:  v1.SchemeGroupVersion.WithResource("revisions"),
			},
			Name: "5554",
		}},
	}}

	for _, test := range table {
		t.Run(test.name, func(t *testing.T) {
			cfgMap := &config.Config{
				RevisionGC: &test.gc,
			}
			runTest(t, cfgMap, revs, cfg, test.wantDeletes)
		})
	}
}

func runTest(
	t *testing.T,
	cfgMap *config.Config,
	revs []*v1.Revision,
	cfg *v1.Configuration,
	wantDeletes []clientgotesting.DeleteActionImpl) {
	t.Helper()
	ctx, _ := SetupFakeContext(t)
	ctx = config.ToContext(ctx, cfgMap)
	client := fakeservingclient.Get(ctx)

	ri := fakerevisioninformer.Get(ctx)
	for _, rev := range revs {
		ri.Informer().GetIndexer().Add(rev)
	}

	recorderList := ActionRecorderList{client}

	Collect(ctx, client, ri.Lister(), cfg)

	actions, err := recorderList.ActionsByVerb()
	if err != nil {
		t.Errorf("Error capturing actions by verb: %q", err)
	}

	for i, want := range wantDeletes {
		if i >= len(actions.Deletes) {
			t.Errorf("Missing delete: %#v", want)
			continue
		}
		got := actions.Deletes[i]
		if got.GetName() != want.GetName() {
			t.Errorf("Unexpected delete[%d]: %#v", i, got)
		}
	}
	if got, want := len(actions.Deletes), len(wantDeletes); got > want {
		for _, extra := range actions.Deletes[want:] {
			t.Errorf("Extra delete: %s/%s", extra.GetNamespace(), extra.GetName())
		}
	}
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
		name: "stale create time",
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
		name: "fresh create time",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(curTime),
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
		want: false,
	}, {
		name: "stale revisionStateModified",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
				Annotations: map[string]string{
					"serving.knative.dev/routingStateModified": staleTime.UTC().Format(time.RFC3339),
				},
			},
		},
		want: true,
	}, {
		name: "fresh revisionStateModified",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "myrev",
				CreationTimestamp: metav1.NewTime(staleTime),
				Annotations: map[string]string{
					"serving.knative.dev/routingStateModified": curTime.UTC().Format(time.RFC3339),
				},
			},
		},
		want: false,
	}}

	cfg := &gc.Config{
		RetainSinceCreateTime:     5 * time.Minute,
		RetainSinceLastActiveTime: 5 * time.Minute,
		MinNonActiveRevisions:     2,
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := isRevisionStale(cfg, test.rev, TestLogger(t))

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

func rev(configName, namespace string, generation int64, ro ...RevisionOption) *v1.Revision {
	config := cfg(configName, namespace, generation)
	rev := resources.MakeRevision(context.Background(), config, clock.RealClock{})
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
