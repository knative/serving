/*
Copyright 2019 The Knative Authors.

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

	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	configurationinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/configuration"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision"
	gcconfig "knative.dev/serving/pkg/gc"
	pkgreconciler "knative.dev/serving/pkg/reconciler"
	configns "knative.dev/serving/pkg/reconciler/gc/config"
)

const (
	controllerAgentName = "revision-gc-controller"
)

// NewController creates a new Garbage Collection controller
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {

	configurationInformer := configurationinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)

	c := &reconciler{
		Base:                pkgreconciler.NewBase(ctx, controllerAgentName, cmw),
		configurationLister: configurationInformer.Lister(),
		revisionLister:      revisionInformer.Lister(),
	}
	impl := controller.NewImpl(c, c.Logger, "Garbage Collection")

	c.Logger.Info("Setting up event handlers")

	// Since the gc controller came from the configuration controller, having event handlers
	// on both configuration and revision matches the existing behaviors of the configuration
	// controller. This is to minimize risk heading into v1.
	// TODO (taragu): probably one or both of these event handlers are not needed
	configurationInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	revisionInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Configuration")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	c.Logger.Info("Setting up ConfigMap receivers with resync func")
	configsToResync := []interface{}{
		&gcconfig.Config{},
	}
	resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		// Triggers syncs on all revisions when configuration changes.
		impl.GlobalResync(revisionInformer.Informer())
	})

	c.Logger.Info("Setting up ConfigMap receivers")
	configStore := configns.NewStore(logging.WithLogger(ctx, c.Logger.Named("config-store")), resync)
	configStore.WatchConfigs(c.ConfigMapWatcher)
	c.configStore = configStore

	return impl
}
