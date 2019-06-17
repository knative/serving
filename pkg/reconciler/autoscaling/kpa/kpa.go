/*
Copyright 2018 The Knative Authors

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

package kpa

import (
	"context"
	"fmt"
	"strconv"

	perrors "github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/autoscaling"
	pav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/autoscaler"
	areconciler "github.com/knative/serving/pkg/reconciler/autoscaling"
	"github.com/knative/serving/pkg/reconciler/autoscaling/config"
	"github.com/knative/serving/pkg/reconciler/autoscaling/kpa/resources"
	resourceutil "github.com/knative/serving/pkg/resources"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// Reconciler tracks PAs and right sizes the ScaleTargetRef based on the
// information from Deciders.
type Reconciler struct {
	*areconciler.Base
	endpointsLister corev1listers.EndpointsLister
	deciders        resources.Deciders
	scaler          *scaler
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// Reconcile right sizes PA ScaleTargetRefs based on the state of decisions in Deciders.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key %s: %v", key, err))
		return nil
	}
	logger := logging.FromContext(ctx)
	ctx = c.ConfigStore.ToContext(ctx)

	logger.Debug("Reconcile kpa-class PodAutoscaler")

	original, err := c.PALister.PodAutoscalers(namespace).Get(name)
	if errors.IsNotFound(err) {
		logger.Debug("PA no longer exists")
		if err := c.deciders.Delete(ctx, namespace, name); err != nil {
			return err
		}
		if err := c.Metrics.Delete(ctx, namespace, name); err != nil {
			return err
		}
		return nil
	} else if err != nil {
		return err
	}

	if original.Class() != autoscaling.KPA {
		logger.Warn("Ignoring non-kpa-class PA")
		return nil
	}

	// Don't modify the informer's copy.
	pa := original.DeepCopy()

	// Reconcile this copy of the pa and then write back any status
	// updates regardless of whether the reconciliation errored out.
	reconcileErr := c.reconcile(ctx, pa)
	if equality.Semantic.DeepEqual(original.Status, pa.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err = c.UpdateStatus(pa); err != nil {
		logger.Warnw("Failed to update kpa status", zap.Error(err))
		c.Recorder.Eventf(pa, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for PA %q: %v", pa.Name, err)
		return err
	}
	if reconcileErr != nil {
		c.Recorder.Event(pa, corev1.EventTypeWarning, "InternalError", reconcileErr.Error())
	}
	return reconcileErr
}

func (c *Reconciler) reconcile(ctx context.Context, pa *pav1alpha1.PodAutoscaler) error {
	logger := logging.FromContext(ctx)

	if pa.GetDeletionTimestamp() != nil {
		return nil
	}

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	pa.SetDefaults(ctx)

	pa.Status.InitializeConditions()
	logger.Debug("PA exists")

	metricSvc, err := c.ReconcileMetricsService(ctx, pa)
	if err != nil {
		return perrors.Wrap(err, "error reconciling metrics service")
	}

	sks, err := c.ReconcileSKS(ctx, pa)
	if err != nil {
		return perrors.Wrap(err, "error reconciling SKS")
	}

	// Since metricSvc is what is being scraped for metrics
	// it should be the correct representation of the pods in the deployment
	// for autoscaling decisions.
	decider, err := c.reconcileDecider(ctx, pa, metricSvc)
	if err != nil {
		return perrors.Wrap(err, "error reconciling decider")
	}

	if err := c.ReconcileMetric(ctx, pa, metricSvc); err != nil {
		return perrors.Wrap(err, "error reconciling metric")
	}

	// Get the appropriate current scale from the metric, and right size
	// the scaleTargetRef based on it.
	want, err := c.scaler.Scale(ctx, pa, decider.Status.DesiredScale)
	if err != nil {
		return perrors.Wrap(err, "error scaling target")
	}

	// Compare the desired and observed resources to determine our situation.
	// We fetch private endpoints here, since for scaling we're interested in the actual
	// state of the deployment.
	got := 0
	// Propagate service name.
	pa.Status.ServiceName = sks.Status.ServiceName
	if sks.Status.IsReady() {
		podCounter := resourceutil.NewScopedEndpointsCounter(c.endpointsLister, pa.Namespace, sks.Status.PrivateServiceName)
		got, err = podCounter.ReadyCount()
		if err != nil {
			return perrors.Wrapf(err, "error checking endpoints %s", sks.Status.PrivateServiceName)
		}
	}
	logger.Infof("PA scale got=%v, want=%v", got, want)

	err = reportMetrics(pa, want, got)
	if err != nil {
		return perrors.Wrap(err, "error reporting metrics")
	}

	// computeActiveCondition decides if we need to change the SKS mode,
	// and returns true if the status has changed.
	if changed := computeActiveCondition(pa, want, got); changed {
		_, err := c.ReconcileSKS(ctx, pa)
		if err != nil {
			return perrors.Wrap(err, "error re-reconciling SKS")
		}
	}
	return nil
}

func (c *Reconciler) reconcileDecider(ctx context.Context, pa *pav1alpha1.PodAutoscaler, k8sSvc string) (*autoscaler.Decider, error) {
	desiredDecider := resources.MakeDecider(ctx, pa, config.FromContext(ctx).Autoscaler, k8sSvc)
	decider, err := c.deciders.Get(ctx, desiredDecider.Namespace, desiredDecider.Name)
	if errors.IsNotFound(err) {
		decider, err = c.deciders.Create(ctx, desiredDecider)
		if err != nil {
			return nil, perrors.Wrap(err, "error creating decider")
		}
	} else if err != nil {
		return nil, perrors.Wrap(err, "error fetching decider")
	}

	// Ignore status when reconciling
	desiredDecider.Status = decider.Status
	if !equality.Semantic.DeepEqual(desiredDecider, decider) {
		decider, err = c.deciders.Update(ctx, desiredDecider)
		if err != nil {
			return nil, perrors.Wrap(err, "error updating decider")
		}
	}

	return decider, nil
}

func reportMetrics(pa *pav1alpha1.PodAutoscaler, want int32, got int) error {
	var serviceLabel string
	var configLabel string
	if pa.Labels != nil {
		serviceLabel = pa.Labels[serving.ServiceLabelKey]
		configLabel = pa.Labels[serving.ConfigurationLabelKey]
	}
	reporter, err := autoscaler.NewStatsReporter(pa.Namespace, serviceLabel, configLabel, pa.Name)
	if err != nil {
		return err
	}

	reporter.ReportActualPodCount(int64(got))
	// Negative "want" values represent an empty metrics pipeline and thus no specific request is being made.
	if want >= 0 {
		reporter.ReportRequestedPodCount(int64(want))
	}
	return nil
}

// computeActiveCondition updates the status of PA, depending on scales desired and present.
// computeActiveCondition returns true if it thinks SKS needs an update.
func computeActiveCondition(pa *pav1alpha1.PodAutoscaler, want int32, got int) (ret bool) {
	minReady := activeThreshold(pa)

	switch {
	case want == 0:
		ret = !pa.Status.IsInactive() // Any state but inactive should change SKS.
		pa.Status.MarkInactive("NoTraffic", "The target is not receiving traffic.")

	case got < minReady && want > 0:
		ret = pa.Status.IsInactive() // If we were inactive and became activating.
		pa.Status.MarkActivating(
			"Queued", "Requests to the target are being buffered as resources are provisioned.")

	case got >= minReady:
		// SKS should already be active.
		pa.Status.MarkActive()

	case want == scaleUnknown:
		// We don't know what scale we want, so don't touch PA at all.
	}

	pa.Status.ObservedGeneration = pa.Generation
	return
}

// activeThreshold returns the scale required for the kpa to be marked Active
func activeThreshold(pa *pav1alpha1.PodAutoscaler) int {
	if min, ok := pa.Annotations[autoscaling.MinScaleAnnotationKey]; ok {
		if ms, err := strconv.Atoi(min); err == nil {
			if ms > 1 {
				return ms
			}
		}
	}

	return 1
}
