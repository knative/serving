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

package stagepodautoscaler

import (
	"context"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	"k8s.io/utils/clock"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	spareconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/stagepodautoscaler"
	palisters "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
)

// Reconciler implements controller.Reconciler for Configuration resources.
type Reconciler struct {
	client clientset.Interface

	// listers index properties about resources
	revisionLister           listers.RevisionLister
	podAutoscalerLister      palisters.PodAutoscalerLister
	stagePodAutoscalerLister listers.StagePodAutoscalerLister
	clock                    clock.PassiveClock
	deploymentLister         appsv1listers.DeploymentLister
}

// Check that our Reconciler implements soreconciler.Interface
var _ spareconciler.Interface = (*Reconciler)(nil)

// ReconcileKind implements Interface.ReconcileKind.
func (c *Reconciler) ReconcileKind(ctx context.Context, spa *v1.StagePodAutoscaler) pkgreconciler.Event {
	ctx, cancel := context.WithTimeout(ctx, pkgreconciler.DefaultTimeout)
	defer cancel()

	logger := logging.FromContext(ctx)

	logger.Info("Check the stage status before before before")

	return nil
}
