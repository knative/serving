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
	"k8s.io/apimachinery/pkg/types"
	clientgotesting "k8s.io/client-go/testing"
	"knative.dev/serving/pkg/autoscaler/metrics"
	servingclientset "knative.dev/serving/pkg/client/clientset/versioned"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	_ "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric/fake"
	metricreconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/metric"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	. "knative.dev/pkg/reconciler/testing"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
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
				return true, nil, apierrs.NewConflict(v1alpha1.Resource("foo"), "bar", errors.New("foo"))
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
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				"failed to initiate or update scraping: the-error"),
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
		retryAttempted = false
		col := &testCollector{}
		if c := ctx.Value(collectorKey{}); c != nil {
			col = c.(*testCollector)
		}
		r := &reconciler{
			Base:      rpkg.NewBase(ctx, controllerAgentName, cmw),
			collector: col,
		}

		return metricreconciler.NewReconciler(ctx, r.Logger, r.ServingClientSet, listers.GetMetricLister(), r.Recorder, r)
	}))
}

func TestReconcileWithCollector(t *testing.T) {
	updateError := errors.New("update error")
	deleteError := errors.New("delete error")

	tests := []struct {
		name                string
		key                 string
		action              func(servingclientset.Interface)
		collector           *testCollector
		createOrUpdateCalls bool
		deleteCalls         bool
		expectErr           error
	}{{
		name: "new",
		key:  "new/metric",
		action: func(scs servingclientset.Interface) {
			m := metric("new", "metric")
			scs.AutoscalingV1alpha1().Metrics(m.Namespace).Create(m)
		},
		collector:           &testCollector{},
		createOrUpdateCalls: true,
	}, {
		name: "delete",
		key:  "old/metric",
		action: func(scs servingclientset.Interface) {
			m := metric("new", "metric")
			scs.AutoscalingV1alpha1().Metrics(m.Namespace).Create(m)
			scs.AutoscalingV1alpha1().Metrics(m.Namespace).Delete(m.Name, &metav1.DeleteOptions{})
		},
		collector:   &testCollector{},
		deleteCalls: true,
	}, {
		name: "error on create",
		key:  "new/metric",
		action: func(scs servingclientset.Interface) {
			m := metric("new", "metric")
			scs.AutoscalingV1alpha1().Metrics(m.Namespace).Create(m)
		},
		collector:           &testCollector{createOrUpdateError: updateError},
		createOrUpdateCalls: true,
		expectErr:           updateError,
	}, {
		name: "error on delete",
		key:  "old/metric",
		action: func(scs servingclientset.Interface) {
			m := metric("new", "metric")
			scs.AutoscalingV1alpha1().Metrics(m.Namespace).Create(m)
			scs.AutoscalingV1alpha1().Metrics(m.Namespace).Delete(m.Name, &metav1.DeleteOptions{})
		},
		collector:   &testCollector{deleteError: deleteError},
		deleteCalls: true,
		expectErr:   deleteError,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel, informers := SetupFakeContextWithCancel(t)

			tt.collector.createOrUpdateCalls = make(chan struct{}, 100)
			tt.collector.deleteCalls = make(chan struct{}, 100)

			scs := servingclient.Get(ctx)

			ctl := NewController(ctx, configmap.NewStaticWatcher(), tt.collector)

			wf, err := controller.RunInformers(ctx.Done(), informers...)
			if err != nil {
				cancel()
				t.Fatalf("StartInformers() = %v", err)
			}

			var eg errgroup.Group
			eg.Go(func() error { return ctl.Run(1, ctx.Done()) })
			defer func() {
				cancel()
				wf()
				eg.Wait()
			}()

			tt.action(scs)

			if !tt.createOrUpdateCalls {
				select {
				case <-tt.collector.createOrUpdateCalls:
					t.Error("CreateOrUpdate() called non-zero times, want 0 times")
				case <-time.After(1 * time.Second):
				}
			} else {
				select {
				case <-tt.collector.createOrUpdateCalls:
				case <-time.After(1 * time.Second):
					t.Error("CreateOrUpdate() called 0 times, want non-zero times")
				}
			}

			if !tt.deleteCalls {
				select {
				case <-tt.collector.deleteCalls:
					t.Error("CreateOrUpdate() called non-zero times, want 0 times")
				case <-time.After(1 * time.Second):
				}
			} else {
				select {
				case <-tt.collector.deleteCalls:
				case <-time.After(1 * time.Second):
					t.Error("CreateOrUpdate() called 0 times, want non-zero times")
				}
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
	createOrUpdateCalls chan struct{}
	createOrUpdateError error

	recordCalls chan struct{}

	deleteCalls chan struct{}
	deleteError error
}

func (c *testCollector) CreateOrUpdate(metric *av1alpha1.Metric) error {
	if c.createOrUpdateCalls != nil {
		c.createOrUpdateCalls <- struct{}{}
	}
	return c.createOrUpdateError
}

func (c *testCollector) Record(key types.NamespacedName, stat metrics.Stat) {
	if c.recordCalls != nil {
		c.recordCalls <- struct{}{}
	}
}

func (c *testCollector) Delete(namespace, name string) error {
	if c.deleteCalls != nil {
		c.deleteCalls <- struct{}{}
	}
	return c.deleteError
}
