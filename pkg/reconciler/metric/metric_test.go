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

	"golang.org/x/sync/errgroup"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgotesting "k8s.io/client-go/testing"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/metrics"
	servingclient "knative.dev/serving/pkg/client/injection/client/fake"
	metricreconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/metric"

	_ "knative.dev/pkg/metrics/testing"
	_ "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric/fake"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
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
	retryAttempted := false
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
	}, {
		Name: "update status with retry",
		Key:  "status/update",
		Objects: []runtime.Object{
			metric("status", "update"),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			func(action clientgotesting.Action) (handled bool, ret runtime.Object, err error) {
				if retryAttempted || !action.Matches("update", "metrics") || action.GetSubresource() != "status" {
					return false, nil, nil
				}
				retryAttempted = true
				resource := schema.GroupResource{
					Group:    "some.group.dev",
					Resource: "resources",
				}
				return true, nil, apierrs.NewConflict(resource, "bar", errors.New("foo"))
			},
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metric("status", "update", ready),
		}, {
			Object: metric("status", "update", ready),
		}},
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
				`Failed to update status for "update-failed": inducing failure for update metrics`),
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
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metric("bad", "collector", failed("CollectionFailed",
				"Failed to reconcile metric collection: the-error")),
		}},
	}, {
		Name: "cannot create collection-part II",
		Ctx: context.WithValue(context.Background(), collectorKey{},
			&testCollector{createOrUpdateError: errors.New("the-error")},
		),
		Key: "bad/collector",
		Objects: []runtime.Object{
			metric("bad", "collector", failed("CollectionFailed",
				"Failed to reconcile metric collection: the-error")),
		},
	}, {
		Name: "no endpoints error",
		Ctx: context.WithValue(context.Background(), collectorKey{},
			&testCollector{createOrUpdateError: metrics.ErrFailedGetEndpoints},
		),
		Key: "bad/collector",
		Objects: []runtime.Object{
			metric("bad", "collector"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metric("bad", "collector", unknown("NoEndpoints",
				metrics.ErrFailedGetEndpoints.Error())),
		}},
	}, {
		Name: "no stats error",
		Ctx: context.WithValue(context.Background(), collectorKey{},
			&testCollector{createOrUpdateError: metrics.ErrDidNotReceiveStat},
		),
		Key: "bad/collector",
		Objects: []runtime.Object{
			metric("bad", "collector"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: metric("bad", "collector", failed("DidNotReceiveStat",
				metrics.ErrDidNotReceiveStat.Error())),
		}},
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		retryAttempted = false
		col := &testCollector{}
		if c := ctx.Value(collectorKey{}); c != nil {
			col = c.(*testCollector)
		}
		r := &reconciler{
			collector: col,
		}

		return metricreconciler.NewReconciler(ctx, logging.FromContext(ctx),
			servingclient.Get(ctx), listers.GetMetricLister(),
			controller.GetEventRecorder(ctx), r)
	}))
}

func TestReconcileWithCollector(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t)

	collector := &testCollector{
		createOrUpdateCalls: make(chan struct{}, 1),
		deleteCalls:         make(chan struct{}, 1),
	}

	ctl := NewController(ctx, configmap.NewStaticWatcher(), collector)
	wf, err := controller.RunInformers(ctx.Done(), informers...)
	if err != nil {
		cancel()
		t.Fatal("StartInformers() =", err)
	}

	barrier := make(chan struct{})
	var eg errgroup.Group
	defer func() {
		cancel()
		wf()
		eg.Wait()
	}()

	eg.Go(func() error {
		close(barrier)
		return ctl.RunContext(ctx, 1)
	})

	m := metric("a-new", "test-metric")
	scs := servingclient.Get(ctx)

	// Ensure the controller is running.
	<-barrier
	scs.AutoscalingV1alpha1().Metrics(m.Namespace).Create(m)
	select {
	case <-collector.createOrUpdateCalls:
		t.Log("Create or Update was invoked")
	case <-time.After(2 * time.Second):
		t.Fatal("CreateOrUpdate() called 0 times, want non-zero times")
	}

	scs.AutoscalingV1alpha1().Metrics(m.Namespace).Delete(m.Name, &metav1.DeleteOptions{})
	select {
	case <-collector.deleteCalls:
		t.Log("Delete was invoked")
	case <-time.After(2 * time.Second):
		t.Error("Delete() called 0 times, want non-zero times")
	}
}

type metricOption func(*av1alpha1.Metric)

func failed(r, m string) metricOption {
	return func(metric *av1alpha1.Metric) {
		metric.Status.MarkMetricFailed(r, m)
	}
}

func unknown(r, m string) metricOption {
	return func(metric *av1alpha1.Metric) {
		metric.Status.MarkMetricNotReady(r, m)
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
	metrics.Collector
	createOrUpdateCalls chan struct{}
	createOrUpdateError error

	deleteCalls chan struct{}
	deleteError error
}

func (c *testCollector) CreateOrUpdate(metric *av1alpha1.Metric) error {
	if c.createOrUpdateCalls != nil {
		c.createOrUpdateCalls <- struct{}{}
	}
	return c.createOrUpdateError
}

func (c *testCollector) Delete(namespace, name string) error {
	if c.deleteCalls != nil {
		c.deleteCalls <- struct{}{}
	}
	return c.deleteError
}

func (c *testCollector) Watch(func(types.NamespacedName)) {}
