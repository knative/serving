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
	"testing"

	"k8s.io/apimachinery/pkg/types"
	"knative.dev/serving/pkg/autoscaler"

	"github.com/pkg/errors"

	metricinformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric/fake"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	logtesting "knative.dev/pkg/logging/testing"
	. "knative.dev/pkg/reconciler/testing"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	rpkg "knative.dev/serving/pkg/reconciler"
	. "knative.dev/serving/pkg/reconciler/testing/v1alpha1"
)

func TestNewController(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, _ := SetupFakeContext(t)
	c := NewController(ctx, configmap.NewStaticWatcher(), &testCollector{})
	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}

func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name:                    "bad workqueue key, Part I",
		Key:                     "too/many/parts",
		SkipNamespaceValidation: true,
	}, {
		Name:                    "bad workqueue key, Part II",
		Key:                     "too-few-parts",
		SkipNamespaceValidation: true,
	}}

	defer logtesting.ClearAll()
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		return &reconciler{
			Base:         rpkg.NewBase(ctx, controllerAgentName, cmw),
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

			if err := r.Reconcile(ctx, tt.key); errors.Cause(err) != tt.expectErr {
				t.Errorf("Reconcile() = %v, wanted %v", errors.Cause(err), tt.expectErr)
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

func metric(namespace, name string) *av1alpha1.Metric {
	return &av1alpha1.Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
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
