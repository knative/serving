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

package service

import (
	"context"

	cfgmap "knative.dev/serving/pkg/apis/config"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	configurationinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	routeinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/route"
	kserviceinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/service"
	ksvcreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/service"

	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	logger := logging.FromContext(ctx)
	serviceInformer := kserviceinformer.Get(ctx)
	routeInformer := routeinformer.Get(ctx)
	configurationInformer := configurationinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)

	logger.Info("Setting up ConfigMap receivers")
	configStore := cfgmap.NewStore(logger.Named("config-store"))
	configStore.WatchConfigs(cmw)

	c := &Reconciler{
		client:              servingclient.Get(ctx),
		configurationLister: configurationInformer.Lister(),
		revisionLister:      revisionInformer.Lister(),
		routeLister:         routeInformer.Lister(),
	}
	opts := func(*controller.Impl) controller.Options {
		return controller.Options{ConfigStore: configStore}
	}
	impl := ksvcreconciler.NewImpl(ctx, c, opts)

	logger.Info("Setting up event handlers")
	serviceInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	handleControllerOf := cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterControllerGK(v1.Kind("Service")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	}
	configurationInformer.Informer().AddEventHandler(handleControllerOf)
	routeInformer.Informer().AddEventHandler(handleControllerOf)

	return impl
}
