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
	"fmt"
	"reflect"

	"go.uber.org/zap"
	"knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler"

	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	listers "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
	rbase "knative.dev/serving/pkg/reconciler"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

const reconcilerName = "Metrics"

// reconciler implements controller.Reconciler for Metric resources.
type reconciler struct {
	*rbase.Base
	collector    autoscaler.Collector
	metricLister listers.MetricLister
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*reconciler)(nil)

// Reconcile compares the actual state with the desired, and attempts to
// converge the two.
func (r *reconciler) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logger.Errorw("Invalid resource key", zap.Error(err))
		return nil
	}

	original, err := r.metricLister.Metrics(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		// The metric object is gone, so delete the collection.
		logger.Info("Stopping to collect metrics")
		return r.collector.Delete(namespace, name)
	} else if err != nil {
		return fmt.Errorf("failed to fetch metric %s: %w", key, err)
	}

	// Don't mess with informer's copy.
	metric := original.DeepCopy()
	metric.SetDefaults(ctx)
	metric.Status.InitializeConditions()

	if err = r.reconcileCollection(ctx, metric); err != nil {
		logger.Errorw("Error reconciling metric collection", zap.Error(err))
		r.Recorder.Event(metric, corev1.EventTypeWarning, "InternalError", err.Error())
	} else {
		metric.Status.MarkMetricReady()
	}

	if !equality.Semantic.DeepEqual(original.Status, metric.Status) {
		// Change of status, need to update the object.
		if uErr := r.updateStatus(original, metric); uErr != nil {
			logger.Warnw("Failed to update metric status", zap.Error(uErr))
			r.Recorder.Eventf(metric, corev1.EventTypeWarning, "UpdateFailed",
				"Failed to update metric status: %v", uErr)
			return uErr
		}
		r.Recorder.Eventf(metric, corev1.EventTypeNormal, "Updated", "Successfully updated metric status %s", key)
	}
	return err
}

func (r *reconciler) reconcileCollection(ctx context.Context, metric *v1alpha1.Metric) error {
	err := r.collector.CreateOrUpdate(metric)
	if err != nil {
		// If create or update failes, we won't be able to collect at all.
		metric.Status.MarkMetricFailed("CollectionFailed", "Failed to reconcile metric collection")
		return fmt.Errorf("failed to initiate or update scraping: %w", err)
	}
	return nil
}

func (r *reconciler) updateStatus(existing *v1alpha1.Metric, desired *v1alpha1.Metric) error {
	existing = existing.DeepCopy()
	return rbase.RetryUpdateConflicts(func(attempts int) (err error) {
		// The first iteration tries to use the informer's state, subsequent attempts fetch the latest state via API.
		if attempts > 0 {
			existing, err = r.ServingClientSet.AutoscalingV1alpha1().Metrics(desired.Namespace).Get(desired.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
		}

		// If there's nothing to update, just return.
		if reflect.DeepEqual(existing.Status, desired.Status) {
			return nil
		}

		existing.Status = desired.Status
		_, err = r.ServingClientSet.AutoscalingV1alpha1().Metrics(existing.Namespace).UpdateStatus(existing)
		return err
	})
}
