/*
Copyright 2019 The Knative Authors

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

package metric

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgotesting "k8s.io/client-go/testing"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/apis/autoscaling"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/autoscaler/metrics"
	servingclient "knative.dev/serving/pkg/client/injection/client/fake"
	metricreconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/metric"

	filteredinformerfactory "knative.dev/pkg/client/injection/kube/informers/factory/filtered"
	_ "knative.dev/pkg/client/injection/kube/informers/factory/filtered/fake"
	. "knative.dev/pkg/reconciler/testing"
	revisionresources "knative.dev/serving/pkg/reconciler/revision/resources"

	metricinformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric/fake"
	autoscalerinformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler/fake"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
)

type collectorKey struct{}

func TestNewController(t *testing.T) {
	ctx, cancel, infs := SetupFakeContextWithCancel(t, func(ctx context.Context) context.Context {
		return filteredinformerfactory.WithSelectors(ctx, serving.RevisionUID)
	})
	waitInformers, err := RunAndSyncInformers(ctx, infs...)
	if err != nil {
		t.Fatal("Error starting up informers:", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()
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
			paLister:  listers.GetPodAutoscalerLister(),
		}

		return metricreconciler.NewReconciler(ctx, logging.FromContext(ctx),
			servingclient.Get(ctx), listers.GetMetricLister(),
			controller.GetEventRecorder(ctx), r)
	}))
}

func TestReconcileWithCollector(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t, func(ctx context.Context) context.Context {
		return filteredinformerfactory.WithSelectors(ctx, serving.RevisionUID)
	})

	collector := &testCollector{}

	ctl := NewController(ctx, configmap.NewStaticWatcher(), collector)
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("RunAndSyncInformers() =", err)
	}

	var eg errgroup.Group
	defer func() {
		cancel()
		wf()
		eg.Wait()
	}()

	eg.Go(func() error {
		return ctl.RunContext(ctx, 1)
	})

	m := metric("a-new", "test-metric")
	a := kpa(m.Namespace, m.Name)

	scs := servingclient.Get(ctx)

	scs.AutoscalingV1alpha1().PodAutoscalers(a.Namespace).Create(ctx, a, metav1.CreateOptions{})
	scs.AutoscalingV1alpha1().Metrics(m.Namespace).Create(ctx, m, metav1.CreateOptions{})

	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		return collector.createOrUpdateCalls.Load() > 0, nil
	}); err != nil {
		t.Fatal("CreateOrUpdate() called 0 times, want non-zero times")
	}

	scs.AutoscalingV1alpha1().Metrics(m.Namespace).Delete(ctx, m.Name, metav1.DeleteOptions{})
	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		return collector.deleteCalls.Load() > 0, nil
	}); err != nil {
		t.Fatal("Delete() called 0 times, want non-zero times")
	}
}

func TestReconcilePaused(t *testing.T) {
	ctx, cancel, informers := SetupFakeContextWithCancel(t, func(ctx context.Context) context.Context {
		return filteredinformerfactory.WithSelectors(ctx, serving.RevisionUID)
	})

	collector := &testCollector{}

	ctl := NewController(ctx, configmap.NewStaticWatcher(), collector)
	wf, err := RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("RunAndSyncInformers() =", err)
	}

	var eg errgroup.Group
	defer func() {
		cancel()
		wf()
		eg.Wait()
	}()

	eg.Go(func() error {
		return ctl.RunContext(ctx, 1)
	})

	m := metric("a-new", "test-metric")
	a := kpa(m.Namespace, m.Name)
	scs := servingclient.Get(ctx)

	scs.AutoscalingV1alpha1().Metrics(m.Namespace).Create(ctx, m, metav1.CreateOptions{})
	metricinformer.Get(ctx).Informer().GetIndexer().Add(m)

	scs.AutoscalingV1alpha1().PodAutoscalers(a.Namespace).Create(ctx, a, metav1.CreateOptions{})
	autoscalerinformer.Get(ctx).Informer().GetIndexer().Add(a)

	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 1*time.Second, false, func(context.Context) (bool, error) {
		return !collector.paused.Load(), nil
	}); err != nil {
		t.Fatal("collector is paused, should not be paused")
	}

	// pause metrics
	a.Status.MetricsPaused = true
	updatePodAutoscaler(t, ctx, a)

	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 1*time.Second, false, func(context.Context) (bool, error) {
		return collector.paused.Load(), nil
	}); err != nil {
		t.Fatal("collector is not paused, should be paused")
	}

	// test when empty revision label found
	m.Labels[serving.RevisionLabelKey] = ""
	updateMetric(t, ctx, m)

	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 1*time.Second, false, func(context.Context) (bool, error) {
		return !collector.paused.Load(), nil
	}); err != nil {
		t.Fatal("collector is paused, should not be paused")
	}
}

type metricOption func(*autoscalingv1alpha1.Metric)

func failed(r, m string) metricOption {
	return func(metric *autoscalingv1alpha1.Metric) {
		metric.Status.MarkMetricFailed(r, m)
	}
}

func unknown(r, m string) metricOption {
	return func(metric *autoscalingv1alpha1.Metric) {
		metric.Status.MarkMetricNotReady(r, m)
	}
}

func ready(m *autoscalingv1alpha1.Metric) {
	m.Status.MarkMetricReady()
}

func updateMetric(
	t *testing.T,
	ctx context.Context,
	m *autoscalingv1alpha1.Metric,
) {
	t.Helper()
	servingclient.Get(ctx).AutoscalingV1alpha1().Metrics(m.Namespace).Update(ctx, m, metav1.UpdateOptions{})
	metricinformer.Get(ctx).Informer().GetIndexer().Update(m)
}

func metric(namespace, name string, opts ...metricOption) *autoscalingv1alpha1.Metric {
	m := &autoscalingv1alpha1.Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				serving.RevisionLabelKey: name,
			},
		},
		Spec: autoscalingv1alpha1.MetricSpec{
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

func newTestRevision(namespace, name string) *v1.Revision {
	return &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  fmt.Sprintf("/apis/ela/v1/namespaces/%s/revisions/%s", namespace, name),
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				autoscaling.ClassAnnotationKey: autoscaling.KPA,
			},
			Labels: map[string]string{
				serving.ServiceLabelKey:       "test-service",
				serving.ConfigurationLabelKey: "test-service",
			},
		},
		Spec: v1.RevisionSpec{},
	}
}

func updatePodAutoscaler(
	t *testing.T,
	ctx context.Context,
	pa *autoscalingv1alpha1.PodAutoscaler,
) {
	t.Helper()
	servingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(pa.Namespace).Update(ctx, pa, metav1.UpdateOptions{})
	autoscalerinformer.Get(ctx).Informer().GetIndexer().Update(pa)
}

func kpa(ns, n string) *autoscalingv1alpha1.PodAutoscaler {
	rev := newTestRevision(ns, n)
	kpa := revisionresources.MakePA(rev, nil)
	kpa.Generation = 1
	kpa.Annotations[autoscaling.ClassAnnotationKey] = "kpa.autoscaling.knative.dev"
	kpa.Annotations[autoscaling.MetricAnnotationKey] = "concurrency"
	kpa.Status.MetricsPaused = false
	kpa.Status.InitializeConditions()
	return kpa
}

type testCollector struct {
	metrics.Collector
	createOrUpdateCalls atomic.Int32
	createOrUpdateError error

	deleteCalls atomic.Int32
	paused      atomic.Bool
}

func (c *testCollector) CreateOrUpdate(metric *autoscalingv1alpha1.Metric) error {
	c.createOrUpdateCalls.Add(1)
	return c.createOrUpdateError
}

func (c *testCollector) Delete(namespace, name string) {
	c.deleteCalls.Add(1)
}

func (c *testCollector) Watch(func(types.NamespacedName)) {}

func (c *testCollector) Pause(metric *autoscalingv1alpha1.Metric) {
	c.paused.Store(true)
}

func (c *testCollector) Resume(metric *autoscalingv1alpha1.Metric) {
	c.paused.Store(false)
}
