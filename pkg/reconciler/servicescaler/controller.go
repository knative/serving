/*
Copyright 2026 The Knative Authors

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

package servicescaler

import (
	"context"

	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	routeinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/route"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	logger := logging.FromContext(ctx)
	routeInformer := routeinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)

	configStore := config.NewStore(logger.Named("config-store"))
	configStore.WatchConfigs(cmw)

	c := &Reconciler{
		client:          servingclient.Get(ctx),
		revisionLister:  revisionInformer.Lister(),
		revisionIndexer: revisionInformer.Informer().GetIndexer(),
		routeLister:     routeInformer.Lister(),
	}
	impl := routereconciler.NewImpl(ctx, c, func(*controller.Impl) controller.Options {
		return controller.Options{
			ConfigStore: configStore,
			// This controller should not mutate the route's status.
			SkipStatusUpdates: true,
		}
	})

	c.tracker = impl.Tracker

	// Make sure trackers are deleted once the observers are removed.
	routeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: impl.Tracker.OnDeletedObserver,
	})

	routeInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	revisionInformer.Informer().AddEventHandler(controller.HandleAll(
		controller.EnsureTypeMeta(
			impl.Tracker.OnChanged,
			v1.SchemeGroupVersion.WithKind("Revision"),
		),
	))

	return impl
}
