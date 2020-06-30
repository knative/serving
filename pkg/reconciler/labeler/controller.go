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

	"k8s.io/client-go/tools/cache"

	"knative.dev/serving/pkg/apis/serving"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	configurationinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	routeinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/route"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"
	servingreconciler "knative.dev/serving/pkg/reconciler"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
)

const controllerAgentName = "labeler-controller"

// NewController wraps a new instance of the labeler that labels
// Configurations with Routes in a controller.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	return NewControllerWithClock(ctx, cmw, system.RealClock{})
}

// NewControllerWithClock includes an override for the clock.
func NewControllerWithClock(
	ctx context.Context,
	cmw configmap.Watcher,
	clock system.Clock,
) *controller.Impl {

	ctx = servingreconciler.AnnotateLoggerWithName(ctx, controllerAgentName)
	logger := logging.FromContext(ctx)
	routeInformer := routeinformer.Get(ctx)
	configInformer := configurationinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)

	c := &Reconciler{
		client:              servingclient.Get(ctx),
		configurationLister: configInformer.Lister(),
		revisionLister:      revisionInformer.Lister(),
		clock:               clock,
	}
	impl := routereconciler.NewImpl(ctx, c)

	logger.Info("Setting up event handlers")
	routeInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	configInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: pkgreconciler.LabelExistsFilterFunc(serving.RouteLabelKey),
		Handler:    controller.HandleAll(impl.EnqueueLabelOfNamespaceScopedResource("", serving.RouteLabelKey)),
	})

	return impl
}
