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
	"time"

	"go.opencensus.io/stats"
	"go.uber.org/zap"

	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/logging"
	pkgmetrics "knative.dev/pkg/metrics"
	"knative.dev/pkg/ptr"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/apis/autoscaling"
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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

const (
	// endpointConvergenceWaitTime is the wait time in the convergence loop
	// to have the number of available endpoints match the expected scale
	endpointConvergenceWaitTime = 500 * time.Millisecond
	// endpointConvergenceTimeout is how long the reconciler is willing to wait
	// for the number of endpoints to converge to the desired scale
	endpointConvergenceTimeout = 1 * time.Minute
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
	logger.Debug("PA exists")

	// We need the SKS object in order to optimize scale to zero
	// performance. It is OK if SKS is nil at this point.
	sksName := anames.SKS(pa.Name)
	sks, err := c.SKSLister.ServerlessServices(pa.Namespace).Get(sksName)
	if err != nil && !errors.IsNotFound(err) {
		logger.Warnw("Error retrieving SKS for Scaler", zap.Error(err))
	}

	// Having an SKS and its PrivateServiceName is a prerequisite for all upcoming steps.
	if sks == nil || (sks != nil && sks.Status.PrivateServiceName == "") {
		if _, err = c.ReconcileSKS(ctx, pa, nv1alpha1.SKSOperationModeServe); err != nil {
			return fmt.Errorf("error reconciling SKS: %w", err)
		}
		return computeStatus(pa, podCounts{want: scaleUnknown})
	}

	pa.Status.MetricsServiceName = sks.Status.PrivateServiceName
	decider, err := c.reconcileDecider(ctx, pa, pa.Status.MetricsServiceName)
	if err != nil {
		return fmt.Errorf("error reconciling Decider: %w", err)
	}

	if err := c.ReconcileMetric(ctx, pa, pa.Status.MetricsServiceName); err != nil {
		return fmt.Errorf("error reconciling Metric: %w", err)
	}

	// Metrics services are no longer needed as we use the private services now.
	if err := c.DeleteMetricsServices(ctx, pa); err != nil {
		return err
	}

	// Get the appropriate current scale from the metric, and right size
	// the scaleTargetRef based on it.
	want, ps, err := c.scaler.calculate(ctx, pa, sks, decider.Status.DesiredScale)
	if err != nil {
		return err
	}

	if decider.Spec.EnableGracefulScaledown {
		podCounter := resourceutil.NewScopedEndpointsCounter(c.endpointsLister, pa.Namespace, pa.Status.MetricsServiceName)
		intReadyCount, err := podCounter.ReadyCount()
		if err != nil {
			return err
		}

		// Check if there is anything to remove
		readyCount := int32(intReadyCount)
		if readyCount > want && decider.Status.RemovalCandidates != nil {
			err := c.markPodsForRemoval(ctx, decider.Status.RemovalCandidates, pa, want, readyCount, podCounter)
			if err != nil {
				return fmt.Errorf("error marking pods for removal: %w", err)
			}
		}
	}

	if err := c.scaler.apply(ctx, pa, ps, want); err != nil {
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
		logger.Infof("SKS should be in proxy mode: want = %d, ebc = %d, PA Inactive? = %v",
			want, decider.Status.ExcessBurstCapacity, pa.Status.IsInactive())
		mode = nv1alpha1.SKSOperationModeProxy
	}

	sks, err = c.ReconcileSKS(ctx, pa, mode)
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

	podCounter := resourceutil.NewNotRunningPodsCounter(c.podsLister, pa.Namespace, pa.Labels[serving.RevisionLabelKey])
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
	return computeStatus(pa, pc)
}

func (c *Reconciler) markPodsForRemoval(ctx context.Context, removalCandidates []string, pa *pav1alpha1.PodAutoscaler, want, readyCount int32, pc resourceutil.EndpointsCounter) error {
	logger := logging.FromContext(ctx)

	pods, err := c.podsLister.Pods(pa.Namespace).List(labels.SelectorFromSet(labels.Set{serving.RevisionUID: pa.Labels[serving.RevisionUID]}))
	if err != nil {
		return err
	}

	podsMap := make(map[string]*corev1.Pod, len(pods))
	for _, p := range pods {
		podsMap[p.Name] = p
	}

	logger.Infof("readyCount = %d, want = %d, removalCandidates = %d", readyCount, want, len(removalCandidates))
	for _, podName := range removalCandidates {
		p := podsMap[podName]
		newP := p.DeepCopy()
		newP.ObjectMeta.Labels[autoscaling.PreferForScaleDownLabelKey] = "true"
		patch, err := duck.CreatePatch(p, newP)
		if err != nil {
			return err
		}

		patchBytes, err := patch.MarshalJSON()
		if err != nil {
			return err
		}

		if _, err = c.KubeClient.CoreV1().Pods(pa.Namespace).Patch(p.ObjectMeta.Name, types.JSONPatchType, patchBytes); err != nil {
			return err
		}
	}

	// Wait for endpoint counts to converge before scaling down
	wait.PollImmediate(endpointConvergenceWaitTime, endpointConvergenceTimeout, func() (bool, error) {
		readyCount, err := pc.ReadyCount()
		if err != nil {
			return false, err
		}

		if int32(readyCount) == want {
			return true, nil
		}

		return false, nil
	})

	return nil
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

func computeStatus(pa *pav1alpha1.PodAutoscaler, pc podCounts) error {
	pa.Status.DesiredScale, pa.Status.ActualScale = ptr.Int32(int32(pc.want)), ptr.Int32(int32(pc.ready))

	if err := reportMetrics(pa, pc); err != nil {
		return fmt.Errorf("error reporting metrics: %w", err)
	}

	computeActiveCondition(pa, pc)

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
