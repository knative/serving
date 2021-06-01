/*
Copyright 2020 The Knative Authors

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

package gc

import (
	"context"
	"time"

	pkgreconciler "knative.dev/pkg/reconciler"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	configreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/configuration"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
)

// reconciler implements controller.Reconciler for garbage collected resources.
type reconciler struct {
	client clientset.Interface

	// listers index properties about resources
	revisionLister listers.RevisionLister
}

// Check that our reconciler implements configreconciler.Interface
var _ configreconciler.Interface = (*reconciler)(nil)

// ReconcileKind implements Interface.ReconcileKind.
func (c *reconciler) ReconcileKind(ctx context.Context, config *v1.Configuration) pkgreconciler.Event {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return collect(ctx, c.client, c.revisionLister, config)
}
