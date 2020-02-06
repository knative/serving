/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metric

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgotesting "k8s.io/client-go/testing"
	"knative.dev/serving/pkg/autoscaler"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	. "knative.dev/pkg/reconciler/testing"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	metricinformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric/fake"
	rpkg "knative.dev/serving/pkg/reconciler"
	. "knative.dev/serving/pkg/reconciler/testing/v1alpha1"
)

type collectorKey struct{}

func TestNewController(t *testing.T) {
	ctx, _ := SetupFakeContext(t)
	c := NewController(ctx, configmap.NewStaticWatcher(), &testCollector{})
	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}

func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name: "bad workqueue key, Part I",
		Key:  "too/many/parts",
	}, {
		Name: "bad workqueue key, Part II",
		Key:  "too-few-parts",
	}, {
		Name: "update status",
		Key:  "status/update",
		Objects: []runtime.Object{
			metric("status", "update"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metric("status", "update", ready),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated",
				"Successfully updated metric status status/update"),
		},
	}, {
		Name: "update status failed",
		Key:  "status/update-failed",
		Objects: []runtime.Object{
			metric("status", "update-failed"),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "metrics"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metric("status", "update-failed", ready),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed",
				"Failed to update metric status: inducing failure for update metrics"),
		},
		WantErr: true,
	}, {
		Name: "cannot create collection-part I",
		Ctx: context.WithValue(context.Background(), collectorKey{},
			&testCollector{createOrUpdateError: errors.New("the-error")},
		),
		Key: "bad/collector",
		Objects: []runtime.Object{
			metric("bad", "collector"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				"failed to initiate or update scraping: the-error"),
			Eventf(corev1.EventTypeNormal, "Updated",
				"Successfully updated metric status bad/collector"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metric("bad", "collector", failed("CollectionFailed",
				"Failed to reconcile metric collection")),
		}},
		WantErr: true,
	}, {
		Name: "cannot create collection-part II",
		Ctx: context.WithValue(context.Background(), collectorKey{},
			&testCollector{createOrUpdateError: errors.New("the-error")},
		),
		Key: "bad/collector",
		Objects: []runtime.Object{
			metric("bad", "collector", failed("CollectionFailed",
				"Failed to reconcile metric collection")),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				"failed to initiate or update scraping: the-error"),
		},
		WantErr: true,
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		col := &testCollector{}
		if c := ctx.Value(collectorKey{}); c != nil {
			col = c.(*testCollector)
		}
		return &reconciler{
			Base:         rpkg.NewBase(ctx, controllerAgentName, cmw),
			collector:    col,
			metricLister: listers.GetMetricLister(),
		}
	}))
}

func TestReconcileWithCollector(t *testing.T) {
	updateError := errors.New("update error")
	deleteError := errors.New("delete error")

	tests := []struct {
		name                string
		key                 string
		metric              *av1alpha1.Metric
		collector           *testCollector
		createOrUpdateCalls int
		deleteCalls         int
		expectErr           error
	}{{
		name:                "new",
		key:                 "new/metric",
		metric:              metric("new", "metric"),
		collector:           &testCollector{},
		createOrUpdateCalls: 1,
	}, {
		name:        "delete",
		key:         "old/metric",
		metric:      metric("new", "metric"),
		collector:   &testCollector{},
		deleteCalls: 1,
	}, {
		name:                "error on create",
		key:                 "new/metric",
		metric:              metric("new", "metric"),
		collector:           &testCollector{createOrUpdateError: updateError},
		createOrUpdateCalls: 1,
		expectErr:           updateError,
	}, {
		name:        "error on delete",
		key:         "old/metric",
		metric:      metric("new", "metric"),
		collector:   &testCollector{deleteError: deleteError},
		deleteCalls: 1,
		expectErr:   deleteError,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := SetupFakeContext(t)
			metricInformer := metricinformer.Get(ctx)

			r := &reconciler{
				Base:         rpkg.NewBase(ctx, controllerAgentName, configmap.NewStaticWatcher()),
				collector:    tt.collector,
				metricLister: metricInformer.Lister(),
			}

			// Make sure the provided metric is available via the fake clients/informers.
			r.ServingClientSet.AutoscalingV1alpha1().Metrics(tt.metric.Namespace).Create(tt.metric)
			metricInformer.Informer().GetIndexer().Add(tt.metric)

			if err := r.Reconcile(ctx, tt.key); !errors.Is(err, tt.expectErr) {
				t.Errorf("Reconcile() = %v, wanted %v", err, tt.expectErr)
			}

			if tt.createOrUpdateCalls != tt.collector.createOrUpdateCalls {
				t.Errorf("CreateOrUpdate() called %d times, want %d times", tt.collector.createOrUpdateCalls, tt.createOrUpdateCalls)
			}
			if tt.deleteCalls != tt.collector.deleteCalls {
				t.Errorf("Delete() called %d times, want %d times", tt.collector.deleteCalls, tt.deleteCalls)
			}
		})
	}
}

type metricOption func(*av1alpha1.Metric)

func failed(r, m string) metricOption {
	return func(metric *av1alpha1.Metric) {
		metric.Status.MarkMetricFailed(r, m)
	}
}

func ready(m *av1alpha1.Metric) {
	m.Status.MarkMetricReady()
}

func metric(namespace, name string, opts ...metricOption) *av1alpha1.Metric {
	m := &av1alpha1.Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: av1alpha1.MetricSpec{
			// Doesn't really matter what is by default, but we need something, so that
			// Spec is not empty.
			StableWindow: time.Minute,
		},
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

type testCollector struct {
	createOrUpdateCalls int
	createOrUpdateError error

	recordCalls int

	deleteCalls int
	deleteError error
}

func (c *testCollector) CreateOrUpdate(metric *av1alpha1.Metric) error {
	c.createOrUpdateCalls++
	return c.createOrUpdateError
}

func (c *testCollector) Record(key types.NamespacedName, stat autoscaler.Stat) {
	c.recordCalls++
}

func (c *testCollector) Delete(namespace, name string) error {
	c.deleteCalls++
	return c.deleteError
}
