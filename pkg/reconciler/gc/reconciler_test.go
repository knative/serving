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

package gc

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/clock"
	clientgotesting "k8s.io/client-go/testing"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	pkgrec "knative.dev/pkg/reconciler"
	apiconfig "knative.dev/serving/pkg/apis/config"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	servingclient "knative.dev/serving/pkg/client/injection/client/fake"
	configreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/configuration"
	"knative.dev/serving/pkg/gc"
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

	controllerOpts := controller.Options{
		ConfigStore: &testConfigStore{
			config: &config.Config{
				RevisionGC: &gc.Config{
					// v1 settings
					StaleRevisionCreateDelay:        5 * time.Minute,
					StaleRevisionTimeout:            5 * time.Minute,
					StaleRevisionMinimumGenerations: 2,

					// v2 settings
					RetainSinceCreateTime:     5 * time.Minute,
					RetainSinceLastActiveTime: 5 * time.Minute,
					MinNonActiveRevisions:     1,
					MaxNonActiveRevisions:     gc.Disabled,
				},
				Features: &apiconfig.Features{
					ResponsiveRevisionGC: apiconfig.Disabled,
				},
			},
		}}

	table := TableTest{{
		Name: "delete oldest, keep two V1",
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
				Resource:  v1.SchemeGroupVersion.WithResource("revisions"),
			},
			Name: "5554",
		}},
		Key: "foo/keep-two",
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		r := &reconciler{
			client:         servingclient.Get(ctx),
			revisionLister: listers.GetRevisionLister(),
		}
		return configreconciler.NewReconciler(ctx, logging.FromContext(ctx),
			servingclient.Get(ctx), listers.GetConfigurationLister(),
			controller.GetEventRecorder(ctx), r, controllerOpts)
	}))
}

func TestGCReconcileV2(t *testing.T) {
	now := time.Now()

	old := now.Add(-11 * time.Minute)
	older := now.Add(-12 * time.Minute)
	oldest := now.Add(-13 * time.Minute)

	controllerOpts := controller.Options{
		ConfigStore: &testConfigStore{
			config: &config.Config{
				RevisionGC: &gc.Config{
					// v1 settings
					StaleRevisionCreateDelay:        5 * time.Minute,
					StaleRevisionTimeout:            5 * time.Minute,
					StaleRevisionMinimumGenerations: 2,

					// v2 settings
					RetainSinceCreateTime:     5 * time.Minute,
					RetainSinceLastActiveTime: 5 * time.Minute,
					MinNonActiveRevisions:     1,
					MaxNonActiveRevisions:     gc.Disabled,
				},
				Features: &apiconfig.Features{
					ResponsiveRevisionGC: apiconfig.Enabled,
				},
			},
		}}

	table := TableTest{{
		Name: "delete oldest, keep two V2",
		Objects: []runtime.Object{
			cfg("keep-two", "foo", 5556,
				WithLatestCreated("5556"),
				WithLatestReady("5556"),
				WithConfigObservedGen),
			rev("keep-two", "foo", 5554, MarkRevisionReady,
				WithRevName("5554"),
				WithRoutingState(v1.RoutingStateReserve),
				WithRoutingStateModified(oldest)),
			rev("keep-two", "foo", 5555, MarkRevisionReady,
				WithRevName("5555"),
				WithRoutingState(v1.RoutingStateReserve),
				WithRoutingStateModified(older)),
			rev("keep-two", "foo", 5556, MarkRevisionReady,
				WithRevName("5556"),
				WithRoutingState(v1.RoutingStateActive),
				WithRoutingStateModified(old)),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "foo",
				Verb:      "delete",
				Resource:  v1.SchemeGroupVersion.WithResource("revisions"),
			},
			Name: "5554",
		}},
		Key: "foo/keep-two",
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		r := &reconciler{
			client:         servingclient.Get(ctx),
			revisionLister: listers.GetRevisionLister(),
		}
		return configreconciler.NewReconciler(ctx, logging.FromContext(ctx),
			servingclient.Get(ctx), listers.GetConfigurationLister(),
			controller.GetEventRecorder(ctx), r, controllerOpts)
	}))
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
