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
	cfgmap "knative.dev/serving/pkg/apis/config"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"
	"knative.dev/serving/pkg/reconciler/configuration/config"
	labelerv1 "knative.dev/serving/pkg/reconciler/labeler/v1"
	labelerv2 "knative.dev/serving/pkg/reconciler/labeler/v2"
)

// Reconciler implements controller.Reconciler for Route resources.
type Reconciler struct {
	caccV1 *labelerv1.Configuration
	raccV1 *labelerv1.Revision

	caccV2 *labelerv2.Configuration
	raccV2 *labelerv2.Revision
}

// Check that our Reconciler implements routereconciler.Interface
var _ routereconciler.Interface = (*Reconciler)(nil)
var _ routereconciler.Finalizer = (*Reconciler)(nil)

// FinalizeKind removes all Route reference metadata from its traffic targets.
// This does not modify or observe spec for the Route itself.
func (rec *Reconciler) FinalizeKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	newGC := config.FromContextOrDefaults(ctx).Features.ResponsiveRevisionGC

	// v1 logic
	if newGC == cfgmap.Disabled || newGC == cfgmap.Allowed {
		if err := labelerv1.ClearLabels(r.Namespace, r.Name, rec.caccV1, rec.raccV1); err != nil {
			return err
		}
	}

	// v2 logic
	if newGC == cfgmap.Allowed || newGC == cfgmap.Enabled {
		if err := labelerv2.ClearRoutingMeta(ctx, r, rec.caccV2, rec.raccV2); err != nil {
			return err
		}
	}

	return nil
}

// ReconcileKind syncs the Route reference metadata to its traffic targets.
// This does not modify or observe spec for the Route itself.
func (rec *Reconciler) ReconcileKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	switch config.FromContextOrDefaults(ctx).Features.ResponsiveRevisionGC {
	case cfgmap.Disabled:
		if err := labelerv1.SyncLabels(r, rec.caccV1, rec.raccV1); err != nil {
			return err
		}
		// Clear the new label for downgrade
		if err := labelerv2.ClearRoutingMeta(ctx, r, rec.caccV2, rec.raccV2); err != nil {
			return err
		}
	case cfgmap.Allowed: // Both labelers on, down/upgrade don't lose data.
		if err := labelerv1.SyncLabels(r, rec.caccV1, rec.raccV1); err != nil {
			return err
		}
		if err := labelerv2.SyncRoutingMeta(ctx, r, rec.caccV2, rec.raccV2); err != nil {
			return err
		}
	case cfgmap.Enabled:
		if err := labelerv2.SyncRoutingMeta(ctx, r, rec.caccV2, rec.raccV2); err != nil {
			return err
		}
		// Clear the old label for upgrade
		if err := labelerv1.ClearLabels(r.Namespace, r.Name, rec.caccV1, rec.raccV1); err != nil {
			return err
		}
	}

	return nil
}
