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
	"k8s.io/utils/clock"
	"knative.dev/pkg/logging"

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
	revisionLister listers.RevisionLister

	clock clock.PassiveClock
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

	so.Status.SetStageRevisionStatus(so.Spec.RevisionTarget)

	logger.Info("Check the stage status")
	logger.Info(so.Status.StageRevisionStatus)
	logger.Info("Check the status")
	logger.Info(so.Status)
	return nil
}
