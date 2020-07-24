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

	// Listers index properties about resources
	configurationLister listers.ConfigurationLister
	revisionLister      listers.RevisionLister
	clock               clock.Clock
}

// Check that our Reconciler implements routereconciler.Interface
var _ routereconciler.Interface = (*Reconciler)(nil)
var _ routereconciler.Finalizer = (*Reconciler)(nil)

func (c *Reconciler) FinalizeKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	newGC := cfgmap.FromContextOrDefaults(ctx).Features.ResponsiveRevisionGC

	// v1 logic
	if newGC == cfgmap.Disabled || newGC == cfgmap.Allowed {
		cacc := labelerv1.NewConfigurationAccessor(c.client, c.tracker, c.configurationLister)
		racc := labelerv1.NewRevisionAccessor(c.client, c.tracker, c.revisionLister)
		if err := labelerv1.ClearLabels(r.Namespace, r.Name, cacc, racc); err != nil {
			return err
		}
	}

	// v2 logic
	if newGC == cfgmap.Allowed || newGC == cfgmap.Enabled {
		cacc := labelerv2.NewConfigurationAccessor(c.client, c.tracker, c.configurationLister, c.clock)
		racc := labelerv2.NewRevisionAccessor(c.client, c.tracker, c.revisionLister, c.clock)
		if err := labelerv2.ClearLabels(r.Namespace, r.Name, cacc, racc); err != nil {
			return err
		}
	}

	return nil
}

func (c *Reconciler) ReconcileKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	newGC := cfgmap.FromContextOrDefaults(ctx).Features.ResponsiveRevisionGC

	// v1 logic
	caccV1 := labelerv1.NewConfigurationAccessor(c.client, c.tracker, c.configurationLister)
	raccV1 := labelerv1.NewRevisionAccessor(c.client, c.tracker, c.revisionLister)
	if newGC == cfgmap.Disabled || newGC == cfgmap.Allowed {
		if err := labelerv1.SyncLabels(r, caccV1, raccV1); err != nil {
			return err
		}
	}

	// v2 logic
	if newGC == cfgmap.Allowed || newGC == cfgmap.Enabled {
		cacc := labelerv2.NewConfigurationAccessor(c.client, c.tracker, c.configurationLister, c.clock)
		racc := labelerv2.NewRevisionAccessor(c.client, c.tracker, c.revisionLister, c.clock)
		if err := labelerv2.SyncLabels(r, cacc, racc); err != nil {
			return err
		}
	}

	if newGC == cfgmap.Enabled {
		if err := labelerv1.ClearLabels(r.Namespace, r.Name, caccV1, raccV1); err != nil {
			return err
		}
	}

	return nil
}
