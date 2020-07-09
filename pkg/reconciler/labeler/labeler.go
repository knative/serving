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
}

// Check that our Reconciler implements routereconciler.Interface
var _ routereconciler.Interface = (*Reconciler)(nil)
var _ routereconciler.Finalizer = (*Reconciler)(nil)

func (c *Reconciler) FinalizeKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	if cfgmap.FromContextOrDefaults(ctx).Features.ResponsiveRevisionGC == cfgmap.Enabled {
		cacc := labelerv2.NewConfigurationAccessor(c.client, c.tracker, c.configurationLister)
		racc := labelerv2.NewRevisionAccessor(c.client, c.tracker, c.revisionLister)
		return labelerv2.ClearLabels(r.Namespace, r.Name, cacc, racc)
	}

	cacc := labelerv1.NewConfigurationAccessor(c.client, c.tracker, c.configurationLister)
	racc := labelerv1.NewRevisionAccessor(c.client, c.tracker, c.revisionLister)
	return labelerv1.ClearLabels(r.Namespace, r.Name, cacc, racc)
}

func (c *Reconciler) ReconcileKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	if cfgmap.FromContextOrDefaults(ctx).Features.ResponsiveRevisionGC == cfgmap.Enabled {
		cacc := labelerv2.NewConfigurationAccessor(c.client, c.tracker, c.configurationLister)
		racc := labelerv2.NewRevisionAccessor(c.client, c.tracker, c.revisionLister)
		return labelerv2.SyncLabels(r, cacc, racc)
	}

	cacc := labelerv1.NewConfigurationAccessor(c.client, c.tracker, c.configurationLister)
	racc := labelerv1.NewRevisionAccessor(c.client, c.tracker, c.revisionLister)
	return labelerv1.SyncLabels(r, cacc, racc)
}
