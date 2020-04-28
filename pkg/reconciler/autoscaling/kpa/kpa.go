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

	"go.opencensus.io/stats"
	"go.uber.org/zap"

	"knative.dev/pkg/logging"
	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/pkg/ptr"
	pkgreconciler "knative.dev/pkg/reconciler"
	pav1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	nv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
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
	corev1listers "k8s.io/client-go/listers/core/v1"
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

	endpointsLister corev1listers.EndpointsLister
	podsLister      corev1listers.PodLister
	deciders        resources.Deciders
	scaler          *scaler
}

// Check that our Reconciler implements pareconciler.Interface
var _ pareconciler.Interface = (*Reconciler)(nil)

func (c *Reconciler) ReconcileKind(ctx context.Context, pa *pav1alpha1.PodAutoscaler) pkgreconciler.Event {
	logger := logging.FromContext(ctx)

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	pa.SetDefaults(ctx)

	pa.Status.InitializeConditions()

	// We need the SKS object in order to optimize scale to zero
	// performance. It is OK if SKS is nil at this point.
	sksName := anames.SKS(pa.Name)
	sks, err := c.SKSLister.ServerlessServices(pa.Namespace).Get(sksName)
	if err != nil && !errors.IsNotFound(err) {
		logger.Warnw("Error retrieving SKS for Scaler", zap.Error(err))
	}

	// Having an SKS and its PrivateServiceName is a prerequisite for all upcoming steps.
	if sks == nil || (sks != nil && sks.Status.PrivateServiceName == "") {
		// Before we can reconcile decider and get real number of activators
		// we start with default of 2.
		if _, err = c.ReconcileSKS(ctx, pa, nv1alpha1.SKSOperationModeServe, 0 /*numActivators == all*/); err != nil {
			return fmt.Errorf("error reconciling SKS: %w", err)
		}
		return computeStatus(pa, podCounts{want: scaleUnknown}, logger)
	}

	pa.Status.MetricsServiceName = sks.Status.PrivateServiceName
	decider, err := c.reconcileDecider(ctx, pa, pa.Status.MetricsServiceName)
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
	if want == 0 || decider.Status.ExcessBurstCapacity < 0 || want == -1 && pa.Status.IsInactive() {
		logger.Infof("SKS should be in proxy mode: want = %d, ebc = %d, #act's = %d PA Inactive? = %v",
			want, decider.Status.ExcessBurstCapacity, decider.Status.NumActivators,
			pa.Status.IsInactive())
		mode = nv1alpha1.SKSOperationModeProxy
	}

	// If we have not successfully reconciled Decider yet, NumActivators will be 0 and
	// we'll use all activators to back this revision.
	sks, err = c.ReconcileSKS(ctx, pa, mode, decider.Status.NumActivators)
	if err != nil {
		return fmt.Errorf("error reconciling SKS: %w", err)
	}

	// Compare the desired and observed resources to determine our situation.
	// We fetch private endpoints here, since for scaling we're interested in the actual
	// state of the deployment.
	ready, notReady := 0, 0

	// Propagate service name.
	pa.Status.ServiceName = sks.Status.ServiceName
	// Currently, SKS.IsReady==True when revision has >0 ready pods.
	if sks.Status.IsReady() {
		podEndpointCounter := resourceutil.NewScopedEndpointsCounter(c.endpointsLister, pa.Namespace, sks.Status.PrivateServiceName)
		ready, err = podEndpointCounter.ReadyCount()
		if err != nil {
			return fmt.Errorf("error checking endpoints %s: %w", sks.Status.PrivateServiceName, err)
		}

		notReady, err = podEndpointCounter.NotReadyCount()
		if err != nil {
			return fmt.Errorf("error checking endpoints %s: %w", sks.Status.PrivateServiceName, err)
		}
	}

	podCounter := resourceutil.NewPodAccessor(c.podsLister, pa.Namespace, pa.Labels[serving.RevisionLabelKey])
	pending, terminating, err := podCounter.PendingTerminatingCount()
	if err != nil {
		return fmt.Errorf("error checking pods for revision %s: %w", pa.Labels[serving.RevisionLabelKey], err)
	}

	logger.Infof("PA scale got=%d, want=%d, ebc=%d", ready, want, decider.Status.ExcessBurstCapacity)

	pc := podCounts{
		want:        int(want),
		ready:       ready,
		notReady:    notReady,
		pending:     pending,
		terminating: terminating,
	}
	logger.Infof("Observed pod counts=%#v", pc)
	return computeStatus(pa, pc, logger)
}

func (c *Reconciler) reconcileDecider(ctx context.Context, pa *pav1alpha1.PodAutoscaler, k8sSvc string) (*scaling.Decider, error) {
	desiredDecider := resources.MakeDecider(ctx, pa, config.FromContext(ctx).Autoscaler, k8sSvc)
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

func computeStatus(pa *pav1alpha1.PodAutoscaler, pc podCounts, logger *zap.SugaredLogger) error {
	pa.Status.DesiredScale, pa.Status.ActualScale = ptr.Int32(int32(pc.want)), ptr.Int32(int32(pc.ready))

	if err := reportMetrics(pa, pc); err != nil {
		return fmt.Errorf("error reporting metrics: %w", err)
	}

	computeActiveCondition(pa, pc)
	logger.Debugf("PA Status after reconcile: %#v", pa.Status.Status)

	pa.Status.ObservedGeneration = pa.Generation
	return nil
}

func reportMetrics(pa *pav1alpha1.PodAutoscaler, pc podCounts) error {
	serviceLabel := pa.Labels[serving.ServiceLabelKey] // This might be empty.
	configLabel := pa.Labels[serving.ConfigurationLabelKey]

	ctx, err := metrics.RevisionContext(pa.Namespace, serviceLabel, configLabel, pa.Name)
	if err != nil {
		return err
	}

	stats := []stats.Measurement{
		actualPodCountM.M(int64(pc.ready)), notReadyPodCountM.M(int64(pc.notReady)),
		pendingPodCountM.M(int64(pc.pending)), terminatingPodCountM.M(int64(pc.terminating)),
	}
	// Negative "want" values represent an empty metrics pipeline and thus no specific request is being made.
	if pc.want >= 0 {
		stats = append(stats, requestedPodCountM.M(int64(pc.want)))
	}
	pkgmetrics.RecordBatch(ctx, stats...)
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
func computeActiveCondition(pa *pav1alpha1.PodAutoscaler, pc podCounts) {
	minReady := activeThreshold(pa)

	switch {
	case pc.want == 0:
		if pa.Status.IsActivating() {
			// We only ever scale to zero while activating if we fail to activate within the progress deadline.
			pa.Status.MarkInactive("TimedOut", "The target could not be activated.")
		} else {
			pa.Status.MarkInactive("NoTraffic", "The target is not receiving traffic.")
		}

	case pc.ready < minReady:
		if pc.want > 0 || !pa.Status.IsInactive() {
			pa.Status.MarkActivating(
				"Queued", "Requests to the target are being buffered as resources are provisioned.")
		}

	case pc.ready >= minReady:
		if pc.want > 0 || !pa.Status.IsInactive() {
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

// resolveScrapeTarget returns metric service name to be scraped based on TBC configuration
// TBC == -1 => activator in path, don't scrape the service
func resolveScrapeTarget(ctx context.Context, pa *pav1alpha1.PodAutoscaler) string {
	tbc := 0.
	if v, ok := pa.TargetBC(); ok {
		tbc = v
	} else {
		tbc = config.FromContext(ctx).Autoscaler.TargetBurstCapacity
	}
	if tbc == -1 {
		return ""
	}
	return pa.Status.MetricsServiceName
}
