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

	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/client-go/tools/cache"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/tracker"
	cfgmap "knative.dev/serving/pkg/apis/config"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
	labelerv1 "knative.dev/serving/pkg/reconciler/labeler/v1"
	labelerv2 "knative.dev/serving/pkg/reconciler/labeler/v2"
)

// Reconciler implements controller.Reconciler for Route resources.
type Reconciler struct {
	client clientset.Interface

	// tracker to track revisions and configurations
	tracker tracker.Interface

	// Indexers index properties about resources
	// Listers provide convenient read access to index
	cLister  listers.ConfigurationLister
	cIndexer cache.Indexer
	rLister  listers.RevisionLister
	rIndexer cache.Indexer

	clock clock.Clock
}

// Check that our Reconciler implements routereconciler.Interface
var _ routereconciler.Interface = (*Reconciler)(nil)
var _ routereconciler.Finalizer = (*Reconciler)(nil)

// FinalizeKind removes all Route reference metadata from its traffic targets.
// This does not modify or observe spec for the Route itself.
func (c *Reconciler) FinalizeKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	newGC := cfgmap.FromContextOrDefaults(ctx).Features.ResponsiveRevisionGC

	// v1 logic
	if newGC == cfgmap.Disabled || newGC == cfgmap.Allowed {
		cacc := labelerv1.NewConfigurationAccessor(c.client, c.tracker, c.cLister)
		racc := labelerv1.NewRevisionAccessor(c.client, c.tracker, c.rLister)
		if err := labelerv1.ClearLabels(r.Namespace, r.Name, cacc, racc); err != nil {
			return err
		}
	}

	// v2 logic
	if newGC == cfgmap.Allowed || newGC == cfgmap.Enabled {
		cacc := labelerv2.NewConfigurationAccessor(c.client, c.tracker, c.cLister, c.cIndexer, c.clock)
		racc := labelerv2.NewRevisionAccessor(c.client, c.tracker, c.rLister, c.rIndexer, c.clock)
		if err := labelerv2.ClearRoutingMeta(r.Namespace, r.Name, cacc, racc); err != nil {
			return err
		}
	}

	return nil
}

// ReconcileKind syncs the Route reference metadata to its traffic targets.
// This does not modify or observe spec for the Route itself.
func (c *Reconciler) ReconcileKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	newGC := cfgmap.FromContextOrDefaults(ctx).Features.ResponsiveRevisionGC

	caccV1 := labelerv1.NewConfigurationAccessor(c.client, c.tracker, c.cLister)
	raccV1 := labelerv1.NewRevisionAccessor(c.client, c.tracker, c.rLister)
	caccV2 := labelerv2.NewConfigurationAccessor(c.client, c.tracker, c.cLister, c.cIndexer, c.clock)
	raccV2 := labelerv2.NewRevisionAccessor(c.client, c.tracker, c.rLister, c.rIndexer, c.clock)

	switch newGC {
	case cfgmap.Disabled:
		if err := labelerv1.SyncLabels(r, caccV1, raccV1); err != nil {
			return err
		}
		// Clear the new label for downgrade
		if err := labelerv2.ClearRoutingMeta(r.Namespace, r.Name, caccV2, raccV2); err != nil {
			return err
		}
	case cfgmap.Allowed: // Both labelers on, down/upgrade don't lose data.
		if err := labelerv1.SyncLabels(r, caccV1, raccV1); err != nil {
			return err
		}
		if err := labelerv2.SyncRoutingMeta(r, caccV2, raccV2); err != nil {
			return err
		}
	case cfgmap.Enabled:
		if err := labelerv2.SyncRoutingMeta(r, caccV2, raccV2); err != nil {
			return err
		}
		// Clear the old label for upgrade
		if err := labelerv1.ClearLabels(r.Namespace, r.Name, caccV1, raccV1); err != nil {
			return err
		}
	}

	return nil
}
