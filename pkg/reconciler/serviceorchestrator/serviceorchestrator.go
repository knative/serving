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

package serviceorchestrator

import (
	"context"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/clock"
	"knative.dev/pkg/logging"
	palisters "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"

	pkgreconciler "knative.dev/pkg/reconciler"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	soreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/serviceorchestrator"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
)

// Reconciler implements controller.Reconciler for Configuration resources.
type Reconciler struct {
	client clientset.Interface

	// listers index properties about resources
	revisionLister           listers.RevisionLister
	podAutoscalerLister      palisters.PodAutoscalerLister
	stagePodAutoscalerLister palisters.StagePodAutoscalerLister
	clock                    clock.PassiveClock
}

// Check that our Reconciler implements soreconciler.Interface
var _ soreconciler.Interface = (*Reconciler)(nil)

// ReconcileKind implements Interface.ReconcileKind.
func (c *Reconciler) ReconcileKind(ctx context.Context, so *v1.ServiceOrchestrator) pkgreconciler.Event {
	ctx, cancel := context.WithTimeout(ctx, pkgreconciler.DefaultTimeout)
	defer cancel()

	logger := logging.FromContext(ctx)
	logger.Info("Reconciling ServiceOrchestrator. Reconciling ServiceOrchestrator. Reconciling ServiceOrchestrator. Reconciling ServiceOrchestrator. Reconciling ServiceOrchestrator.")

	logger.Info("Check the stage status before before before")
	logger.Info(so.Status.StageRevisionStatus)

	// If spec.StageRevisionStatus is nil, do nothing.
	if so.Spec.StageRevisionTarget == nil || len(so.Spec.StageRevisionTarget) == 0 {
		return nil
	}

	// Create the stagePodAutoscaler for the revision to be scaled up
	for _, revision := range so.Spec.StageRevisionTarget {
		if revision.Direction == "" || revision.Direction == "up" {
			spa, err := c.stagePodAutoscalerLister.StagePodAutoscalers(so.Namespace).Get(revision.RevisionName)
			if apierrs.IsNotFound(err) {
				c.createStagePA(ctx, &revision)
				return nil
			} else if err != nil {
				return nil
			} else {
				c.client.AutoscalingV1alpha1().StagePodAutoscalers(so.Namespace).Update(ctx, spa, metav1.UpdateOptions{})
			}
		}
	}

	// If spec.StageRevisionStatus is nil, check on if the number of replicas meets the conditions.
	if so.IsStageInProgress() {
		if !c.checkStageScaleUpReady(so) {
			// Create the stage pod autoscaler with the new maxScale set to
			// maxScale defined in the revision traffic, because scale up phase is not over, we cannot
			// scale down the old revision.

			return nil
		}

		so.Status.MarkStageRevisionScaleUpReady()
		// Create the stage pod autoscaler with the new maxScale set to targetScale defined
		// in the revision traffic. Scaling up phase is over, we are able to scale down.

		if !c.checkStageScaleDownReady(so) {
			return nil
		}

		so.Status.MarkStageRevisionScaleDownReady()

		// When the number of replicas of the new and old revision meets the conditions, set the status to stage ready.
		so.Status.SetStageRevisionStatus(so.Spec.StageRevisionTarget)
		so.Status.MarkStageRevisionReady()
		if equality.Semantic.DeepEqual(so.Status.StageRevisionStatus, so.Spec.RevisionTarget) {
			so.Status.MarkLastStageRevisionComplete()
		}
		return nil
	} else if so.IsStageReady() {
		if so.IsInProgress() {
			if !equality.Semantic.DeepEqual(so.Status.StageRevisionStatus, so.Spec.StageRevisionTarget) {
				// Start to move to a new stage.
				so.Status.MarkStageRevisionScaleUpInProgress("StageRevisionStart", "Start to roll out a new stage.")
				so.Status.MarkStageRevisionScaleDownInProgress("StageRevisionStart", "Start to roll out a new stage.")
				so.Status.MarkStageRevisionInProgress("StageRevisionStart", "Start to roll out a new stage.")
			}
		}
	}

	return nil
}

func (c *Reconciler) createStagePA(ctx context.Context, revision *v1.RevisionTarget) error {

	return nil
}

func (c *Reconciler) checkStageScaleUpReady(so *v1.ServiceOrchestrator) bool {
	for _, revision := range so.Spec.StageRevisionTarget {
		pa, err := c.podAutoscalerLister.PodAutoscalers(so.Namespace).Get(revision.RevisionName)
		if err != nil {
			return false
		}
		if revision.Direction == "" || revision.Direction == "up" {
			if revision.TargetReplicas == nil {
				return false
			}

			if *pa.Status.DesiredScale >= *revision.MinScale && *pa.Status.ActualScale >= *revision.MinScale {
				if *pa.Status.DesiredScale >= *revision.TargetReplicas && *pa.Status.ActualScale >= *revision.TargetReplicas {
					return true
				} else if *pa.Status.DesiredScale < *revision.TargetReplicas && *pa.Status.ActualScale < *revision.TargetReplicas {
					if *pa.Status.DesiredScale == *pa.Status.ActualScale {
						return true
					}
				}
			}
		}

	}
	return false
}

func (c *Reconciler) checkStageScaleDownReady(so *v1.ServiceOrchestrator) bool {
	for _, revision := range so.Spec.StageRevisionTarget {
		pa, err := c.podAutoscalerLister.PodAutoscalers(so.Namespace).Get(revision.RevisionName)
		if err != nil {
			return false
		}
		if revision.Direction == "down" {
			if revision.TargetReplicas == nil {
				return false
			}
			if *pa.Status.DesiredScale > *revision.TargetReplicas || *pa.Status.ActualScale > *revision.TargetReplicas {
				return false
			}
		}
	}
	return true
}
