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

	perrors "github.com/pkg/errors"
	"go.uber.org/zap"

	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	pav1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	nv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/autoscaler"
	areconciler "knative.dev/serving/pkg/reconciler/autoscaling"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	"knative.dev/serving/pkg/reconciler/autoscaling/kpa/resources"
	anames "knative.dev/serving/pkg/reconciler/autoscaling/resources/names"
	resourceutil "knative.dev/serving/pkg/resources"

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
		return nil
	} else if err != nil {
		return err
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
		logger.Warnw("Failed to update pa status", zap.Error(err))
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

	// We need the SKS object in order to optimize scale to zero
	// performance. It is OK if SKS is nil at this point.
	sksName := anames.SKS(pa.Name)
	sks, err := c.SKSLister.ServerlessServices(pa.Namespace).Get(sksName)
	if err != nil && !errors.IsNotFound(err) {
		logger.Warnw("Error retrieving SKS for Scaler", zap.Error(err))
	}

	// Get the appropriate current scale from the metric, and right size
	// the scaleTargetRef based on it.
	want, err := c.scaler.Scale(ctx, pa, sks, decider.Status.DesiredScale)
	if err != nil {
		return perrors.Wrap(err, "error scaling target")
	}

	mode := nv1alpha1.SKSOperationModeServe
	// We put activator in the serving path in two cases:
	// 1. The revision is scaled to 0.
	// 2. The excess burst capacity is negative.
	if want == 0 || decider.Status.ExcessBurstCapacity < 0 {
		logger.Infof("SKS is in proxy mode: want = %d, ebc = %d", want, decider.Status.ExcessBurstCapacity)
		logger.Debugf("SKS is in proxy mode: want = %d, ebc = %d", want, decider.Status.ExcessBurstCapacity)
		mode = nv1alpha1.SKSOperationModeProxy
	}

	sks, err = c.ReconcileSKS(ctx, pa, mode)
	if err != nil {
		return perrors.Wrap(err, "error reconciling SKS")
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
	logger.Infof("PA scale got=%d, want=%d", got, want)
	pa.Status.DesiredScale, pa.Status.ActualScale = &want, ptr.Int32(int32(got))

	err = reportMetrics(pa, want, got)
	if err != nil {
		return perrors.Wrap(err, "error reporting metrics")
	}

	computeActiveCondition(pa, want, got)

	pa.Status.ObservedGeneration = pa.Generation
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

// computeActiveCondition updates the status of a PA given the current scale (got), desired scale (want)
// and the current status, as per the following table:
//
//    | Want | Got    | Status     | New status |
//    | 0    | <any>  | <any>      | inactive   |
//    | >0   | < min  | <any>      | activating |
//    | >0   | >= min | <any>      | active     |
//    | -1   | < min  | inactive   | inactive   |
//    | -1   | < min  | activating | activating |
//    | -1   | < min  | active     | activating |
//    | -1   | >= min | inactive   | inactive   |
//    | -1   | >= min | activating | active     |
//    | -1   | >= min | active     | active     |
func computeActiveCondition(pa *pav1alpha1.PodAutoscaler, want int32, got int) {
	minReady := activeThreshold(pa)

	switch {
	case want == 0:
		if pa.Status.IsActivating() {
			// We only ever scale to zero while activating if we fail to activate within the progress deadline.
			pa.Status.MarkInactive("TimedOut", "The target could not be activated.")
		} else {
			pa.Status.MarkInactive("NoTraffic", "The target is not receiving traffic.")
		}

	case got < minReady:
		if want > 0 || !pa.Status.IsInactive() {
			pa.Status.MarkActivating(
				"Queued", "Requests to the target are being buffered as resources are provisioned.")
		}

	case got >= minReady:
		if want > 0 || !pa.Status.IsInactive() {
			// SKS should already be active.
			pa.Status.MarkActive()
		}
	}
}

// activeThreshold returns the scale required for the pa to be marked Active
func activeThreshold(pa *pav1alpha1.PodAutoscaler) int {
	min, _ := pa.ScaleBounds()
	if min < 1 {
		min = 1
	}

	return int(min)
}
