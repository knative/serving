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
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
)

// Reconciler implements controller.Reconciler for Route resources.
type Reconciler struct {
	client clientset.Interface

	// Listers index properties about resources
	configurationLister listers.ConfigurationLister
	revisionLister      listers.RevisionLister
}

// Check that our Reconciler implements routereconciler.Interface
var _ routereconciler.Interface = (*Reconciler)(nil)
var _ routereconciler.Finalizer = (*Reconciler)(nil)

func (c *Reconciler) FinalizeKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	return c.clearLabels(ctx, r.Namespace, r.Name)
}

func (c *Reconciler) ReconcileKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	return c.syncLabels(ctx, r)
}
