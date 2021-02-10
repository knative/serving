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

package labeler

import (
	"context"

	pkgreconciler "knative.dev/pkg/reconciler"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"
)

// Reconciler implements controller.Reconciler for Route resources.
type Reconciler struct {
	caccV2 *ConfigurationAccessor
	raccV2 *RevisionAccessor
}

// Check that our Reconciler implements routereconciler.Interface
var _ routereconciler.Interface = (*Reconciler)(nil)
var _ routereconciler.Finalizer = (*Reconciler)(nil)

// FinalizeKind removes all Route reference metadata from its traffic targets.
// This does not modify or observe spec for the Route itself.
func (rec *Reconciler) FinalizeKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	if err := clearRoutingMeta(ctx, r, rec.caccV2, rec.raccV2); err != nil {
		return err
	}
	return nil
}

// ReconcileKind syncs the Route reference metadata to its traffic targets.
// This does not modify or observe spec for the Route itself.
func (rec *Reconciler) ReconcileKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	if err := syncRoutingMeta(ctx, r, rec.caccV2, rec.raccV2); err != nil {
		return err
	}

	return nil
}
