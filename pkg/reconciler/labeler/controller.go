/*
Copyright 2019 The Knative Authors

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

	servingclient "knative.dev/serving/pkg/client/injection/client"
	configurationinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	routeinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/route"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"
	servingreconciler "knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/configuration/config"
	labelerv1 "knative.dev/serving/pkg/reconciler/labeler/v1"
	labelerv2 "knative.dev/serving/pkg/reconciler/labeler/v2"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/tracker"
)

const controllerAgentName = "labeler-controller"

// NewController wraps a new instance of the labeler that labels
// Configurations with Routes in a controller.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	return newControllerWithClock(ctx, cmw, clock.RealClock{})
}

func newControllerWithClock(
	ctx context.Context,
	cmw configmap.Watcher,
	clock clock.Clock,
) *controller.Impl {
	ctx = servingreconciler.AnnotateLoggerWithName(ctx, controllerAgentName)
	logger := logging.FromContext(ctx)
	routeInformer := routeinformer.Get(ctx)
	configInformer := configurationinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)

	logger.Info("Setting up ConfigMap receivers")
	configStore := config.NewStore(logger.Named("config-store"))
	configStore.WatchConfigs(cmw)

	c := &Reconciler{}
	impl := routereconciler.NewImpl(ctx, c, func(*controller.Impl) controller.Options {
		return controller.Options{
			ConfigStore: configStore,
			// The labeler shouldn't mutate the route's status.
			SkipStatusUpdates: true,
		}
	})

	logger.Info("Setting up event handlers")
	routeInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	tracker := tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))

	// Make sure trackers are deleted once the observers are removed.
	routeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: tracker.OnDeletedObserver,
	})

	configInformer.Informer().AddEventHandler(controller.HandleAll(tracker.OnChanged))
	revisionInformer.Informer().AddEventHandler(controller.HandleAll(tracker.OnChanged))

	client := servingclient.Get(ctx)

	c.caccV1 = labelerv1.NewConfigurationAccessor(client, tracker, configInformer.Lister())
	c.caccV2 = labelerv2.NewConfigurationAccessor(client, tracker, configInformer.Lister(), configInformer.Informer().GetIndexer(), clock)

	c.raccV1 = labelerv1.NewRevisionAccessor(client, tracker, revisionInformer.Lister())
	c.raccV2 = labelerv2.NewRevisionAccessor(client, tracker, revisionInformer.Lister(), revisionInformer.Informer().GetIndexer(), clock)

	return impl
}
