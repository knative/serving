/*
Copyright 2018 The Knative Authors

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

package kpa

import (
	"context"
	"fmt"
	"math"

	"go.opencensus.io/stats"
	"go.uber.org/zap"

	nv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/logging"
	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/pkg/ptr"
	pkgreconciler "knative.dev/pkg/reconciler"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/autoscaler/scaling"
	pareconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/podautoscaler"
	"knative.dev/serving/pkg/metrics"
	areconciler "knative.dev/serving/pkg/reconciler/autoscaling"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	"knative.dev/serving/pkg/reconciler/autoscaling/kpa/resources"
	anames "knative.dev/serving/pkg/reconciler/autoscaling/resources/names"
	resourceutil "knative.dev/serving/pkg/resources"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

const (
	noPrivateServiceName = "No Private Service Name"
	noTrafficReason      = "NoTraffic"
	minActivators        = 2
)

// podCounts keeps record of various numbers of pods
// for each revision.
type podCounts struct {
	want        int
	ready       int
	notReady    int
	pending     int
	terminating int
}

// Reconciler tracks PAs and right sizes the ScaleTargetRef based on the
// information from Deciders.
type Reconciler struct {
	*areconciler.Base

	podsLister corev1listers.PodLister
	deciders   resources.Deciders
	scaler     *scaler
}

// Check that our Reconciler implements the necessary interfaces.
var (
	_ pareconciler.Interface            = (*Reconciler)(nil)
	_ pkgreconciler.OnDeletionInterface = (*Reconciler)(nil)
)

// ReconcileKind implements Interface.ReconcileKind.
func (c *Reconciler) ReconcileKind(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler) pkgreconciler.Event {
	logger := logging.FromContext(ctx)

	// We need the SKS object in order to optimize scale to zero
	// performance. It is OK if SKS is nil at this point.
	sksName := anames.SKS(pa.Name)
	sks, err := c.SKSLister.ServerlessServices(pa.Namespace).Get(sksName)
	if err != nil && !errors.IsNotFound(err) {
		logger.Warnw("Error retrieving SKS for Scaler", zap.Error(err))
	}

	// Having an SKS and its PrivateServiceName is a prerequisite for all upcoming steps.
	if sks == nil || sks.Status.PrivateServiceName == "" {
		// Before we can reconcile decider and get real number of activators
		// we start with default of 2.
		if _, err = c.ReconcileSKS(ctx, pa, nv1alpha1.SKSOperationModeServe, minActivators); err != nil {
			return fmt.Errorf("error reconciling SKS: %w", err)
		}
		pa.Status.MarkSKSNotReady(noPrivateServiceName) // In both cases this is true.
		computeStatus(ctx, pa, podCounts{want: scaleUnknown}, logger)
		return nil
	}

	pa.Status.MetricsServiceName = sks.Status.PrivateServiceName
	decider, err := c.reconcileDecider(ctx, pa)
	if err != nil {
		return fmt.Errorf("error reconciling Decider: %w", err)
	}

	if err := c.ReconcileMetric(ctx, pa, resolveScrapeTarget(ctx, pa)); err != nil {
		return fmt.Errorf("error reconciling Metric: %w", err)
	}

	// Get the appropriate current scale from the metric, and right size
	// the scaleTargetRef based on it.
	want, err := c.scaler.scale(ctx, pa, sks, decider.Status.DesiredScale)
	if err != nil {
		return fmt.Errorf("error scaling target: %w", err)
	}

	mode := nv1alpha1.SKSOperationModeServe
	// We put activator in the serving path in the following cases:
	// 1. The revision is scaled to 0:
	//   a. want == 0
	//   b. want == -1 && PA is inactive (Autoscaler has no previous knowledge of
	//			this revision, e.g. after a restart) but PA status is inactive (it was
	//			already scaled to 0).
	// 2. The excess burst capacity is negative.
	if want == 0 || decider.Status.ExcessBurstCapacity < 0 || want == scaleUnknown && pa.Status.IsInactive() {
		mode = nv1alpha1.SKSOperationModeProxy
	}

	// Compare the desired and observed resources to determine our situation.
	podCounter := resourceutil.NewPodAccessor(c.podsLister, pa.Namespace, pa.Labels[serving.RevisionLabelKey])
	ready, notReady, pending, terminating, err := podCounter.PodCountsByState()
	if err != nil {
		return fmt.Errorf("error getting pod counts: %w", err)
	}

	// Determine the amount of activators to put into the routing path.
	numActivators := computeNumActivators(ready, decider)

	logger.Infof("SKS should be in %s mode: want = %d, ebc = %d, #act's = %d PA Inactive? = %v",
		mode, want, decider.Status.ExcessBurstCapacity, numActivators,
		pa.Status.IsInactive())

	sks, err = c.ReconcileSKS(ctx, pa, mode, numActivators)
	if err != nil {
		return fmt.Errorf("error reconciling SKS: %w", err)
	}
	// Propagate service name.
	pa.Status.ServiceName = sks.Status.ServiceName

	// If SKS is not ready — ensure we're not becoming ready.
	if sks.IsReady() {
		logger.Debug("SKS is ready, marking SKS status ready")
		pa.Status.MarkSKSReady()
	} else {
		logger.Debug("SKS is not ready, marking SKS status not ready")
		pa.Status.MarkSKSNotReady(sks.Status.GetCondition(nv1alpha1.ServerlessServiceConditionReady).GetMessage())
	}

	logger.Infof("PA scale got=%d, want=%d, desiredPods=%d ebc=%d", ready, want,
		decider.Status.DesiredScale, decider.Status.ExcessBurstCapacity)

	pc := podCounts{
		want:        int(want),
		ready:       ready,
		notReady:    notReady,
		pending:     pending,
		terminating: terminating,
	}
	logger.Infof("Observed pod counts=%#v", pc)
	computeStatus(ctx, pa, pc, logger)
	return nil
}

// ObserveDeletion implements OnDeletionInterface.ObserveDeletion.
func (c *Reconciler) ObserveDeletion(ctx context.Context, key types.NamespacedName) error {
	c.deciders.Delete(ctx, key.Namespace, key.Name)
	return nil
}

func (c *Reconciler) reconcileDecider(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler) (*scaling.Decider, error) {
	desiredDecider := resources.MakeDecider(pa, config.FromContext(ctx).Autoscaler)
	decider, err := c.deciders.Get(ctx, desiredDecider.Namespace, desiredDecider.Name)
	if errors.IsNotFound(err) {
		decider, err = c.deciders.Create(ctx, desiredDecider)
		if err != nil {
			return nil, fmt.Errorf("error creating Decider: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("error fetching Decider: %w", err)
	}

	// Ignore status when reconciling
	desiredDecider.Status = decider.Status
	if !equality.Semantic.DeepEqual(desiredDecider, decider) {
		decider, err = c.deciders.Update(ctx, desiredDecider)
		if err != nil {
			return nil, fmt.Errorf("error updating decider: %w", err)
		}
	}
	return decider, nil
}

func computeStatus(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler, pc podCounts, logger *zap.SugaredLogger) {
	pa.Status.DesiredScale, pa.Status.ActualScale = ptr.Int32(int32(pc.want)), ptr.Int32(int32(pc.ready))

	reportMetrics(pa, pc)
	computeActiveCondition(ctx, pa, pc)
	logger.Debugf("PA Status after reconcile: %#v", pa.Status.Status)
}

func reportMetrics(pa *autoscalingv1alpha1.PodAutoscaler, pc podCounts) {
	serviceLabel := pa.Labels[serving.ServiceLabelKey] // This might be empty.
	configLabel := pa.Labels[serving.ConfigurationLabelKey]

	ctx := metrics.RevisionContext(pa.Namespace, serviceLabel, configLabel, pa.Name)

	stats := []stats.Measurement{
		actualPodCountM.M(int64(pc.ready)), notReadyPodCountM.M(int64(pc.notReady)),
		pendingPodCountM.M(int64(pc.pending)), terminatingPodCountM.M(int64(pc.terminating)),
	}
	// Negative "want" values represent an empty metrics pipeline and thus no specific request is being made.
	if pc.want >= 0 {
		stats = append(stats, requestedPodCountM.M(int64(pc.want)))
	}
	pkgmetrics.RecordBatch(ctx, stats...)
}

// computeActiveCondition updates the status of a PA given the current scale (got), desired scale (want)
// active threshold (min), and the current status, as per the following table:
//
//    | Want | Got    | min   | Status     | New status |
//    | 0    | <any>  | <any> | <any>      | inactive   |
//    | >0   | < min  | <any> | <any>      | activating |
//    | >0   | >= min | <any> | <any>      | active     |
//    | -1   | < min  | <any> | inactive   | inactive   |
//    | -1   | < min  | <any> | activating | activating |
//    | -1   | < min  | <any> | active     | activating |
//    | -1   | >= min | <any> | inactive   | inactive   |
//    | -1   | >= min | 0     | activating | inactive   |
//    | -1   | >= min | 0     | active     | inactive   | <-- this case technically is impossible.
//    | -1   | >= min | >0    | activating | active     |
//    | -1   | >= min | >0    | active     | active     |
func computeActiveCondition(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler, pc podCounts) {
	minReady := activeThreshold(ctx, pa)
	if pc.ready >= minReady && pa.Status.ServiceName != "" {
		pa.Status.MarkScaleTargetInitialized()
	}

	switch {
	// Need to check for minReady = 0 because in the initialScale 0 case, pc.want will be -1.
	case pc.want == 0 || minReady == 0:
		if pa.Status.IsActivating() && minReady > 0 {
			// We only ever scale to zero while activating if we fail to activate within the progress deadline.
			pa.Status.MarkInactive("TimedOut", "The target could not be activated.")
		} else {
			pa.Status.MarkInactive(noTrafficReason, "The target is not receiving traffic.")
		}

	case pc.ready < minReady:
		if pc.want > 0 || !pa.Status.IsInactive() {
			pa.Status.MarkActivating(
				"Queued", "Requests to the target are being buffered as resources are provisioned.")
		} else {
			// This is for the initialScale 0 case. In the first iteration, minReady is 0,
			// but for the following iterations, minReady is 1. pc.want will continue being
			// -1 until we start receiving metrics, so we will end up here.
			// Even though PA is already been marked as inactive in the first iteration, we
			// still need to set it again. Otherwise reconciliation will fail with NewObservedGenFailure
			// because we cannot go through one iteration of reconciliation without setting
			// some status.
			pa.Status.MarkInactive(noTrafficReason, "The target is not receiving traffic.")
		}

	case pc.ready >= minReady:
		if pc.want > 0 || !pa.Status.IsInactive() {
			pa.Status.MarkActive()
		}
	}
}

// activeThreshold returns the scale required for the pa to be marked Active
func activeThreshold(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler) int {
	asConfig := config.FromContext(ctx).Autoscaler
	min, _ := pa.ScaleBounds(asConfig)
	if !pa.Status.IsScaleTargetInitialized() {
		initialScale := resources.GetInitialScale(asConfig, pa)
		return int(intMax(min, initialScale))
	}
	return int(intMax(min, 1))
}

// resolveScrapeTarget returns metric service name to be scraped based on TBC configuration
// TBC == -1 => activator in path, don't scrape the service
func resolveScrapeTarget(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler) string {
	tbc := resolveTBC(ctx, pa)
	if tbc == -1 {
		return ""
	}

	return pa.Status.MetricsServiceName
}

func resolveTBC(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler) float64 {
	if v, ok := pa.TargetBC(); ok {
		return v
	}

	return config.FromContext(ctx).Autoscaler.TargetBurstCapacity
}

func intMax(a, b int32) int32 {
	if a < b {
		return b
	}
	return a
}

func computeNumActivators(readyPods int, decider *scaling.Decider) int32 {
	if decider.Spec.TargetBurstCapacity == 0 {
		return int32(minActivators)
	}

	capacityToCover := float64(readyPods) * decider.Spec.TotalValue

	// Add the respective TargetBurstCapacity to calculate the amount of activators
	// needed to cover that.
	if decider.Spec.TargetBurstCapacity > 0 {
		capacityToCover += decider.Spec.TargetBurstCapacity
	}

	return int32(math.Max(minActivators, math.Ceil(capacityToCover/decider.Spec.ActivatorCapacity)))
}
