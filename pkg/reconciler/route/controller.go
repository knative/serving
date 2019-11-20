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

package route

import (
	"context"

	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	certificateinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/certificate"
	ingressinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/ingress"
	configurationinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/configuration"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision"
	routeinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/route"

	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
	"knative.dev/pkg/tracker"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/route/config"
)

const (
	controllerAgentName = "route-controller"
)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	return NewControllerWithClock(ctx, cmw, system.RealClock{})
}

func NewControllerWithClock(
	ctx context.Context,
	cmw configmap.Watcher,
	clock system.Clock,
) *controller.Impl {

	serviceInformer := serviceinformer.Get(ctx)
	routeInformer := routeinformer.Get(ctx)
	configInformer := configurationinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)
	ingressInformer := ingressinformer.Get(ctx)
	certificateInformer := certificateinformer.Get(ctx)

	// No need to lock domainConfigMutex yet since the informers that can modify
	// domainConfig haven't started yet.
	c := &Reconciler{
		Base:                reconciler.NewBase(ctx, controllerAgentName, cmw),
		routeLister:         routeInformer.Lister(),
		configurationLister: configInformer.Lister(),
		revisionLister:      revisionInformer.Lister(),
		serviceLister:       serviceInformer.Lister(),
		ingressLister:       ingressInformer.Lister(),
		certificateLister:   certificateInformer.Lister(),
		clock:               clock,
	}
	impl := controller.NewImpl(c, c.Logger, "Routes")

	c.Logger.Info("Setting up event handlers")
	routeInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	serviceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Route")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	ingressInformer.Informer().AddEventHandler(controller.HandleAll(impl.EnqueueControllerOf))

	c.tracker = tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))

	configInformer.Informer().AddEventHandler(controller.HandleAll(
		// Call the tracker's OnChanged method, but we've seen the objects
		// coming through this path missing TypeMeta, so ensure it is properly
		// populated.
		controller.EnsureTypeMeta(
			c.tracker.OnChanged,
			v1alpha1.SchemeGroupVersion.WithKind("Configuration"),
		),
	))

	revisionInformer.Informer().AddEventHandler(controller.HandleAll(
		// Call the tracker's OnChanged method, but we've seen the objects
		// coming through this path missing TypeMeta, so ensure it is properly
		// populated.
		controller.EnsureTypeMeta(
			c.tracker.OnChanged,
			v1alpha1.SchemeGroupVersion.WithKind("Revision"),
		),
	))

	certificateInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Route")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	c.Logger.Info("Setting up ConfigMap receivers")
	configsToResync := []interface{}{
		&network.Config{},
		&config.Domain{},
	}
	resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		impl.GlobalResync(routeInformer.Informer())
	})
	configStore := config.NewStore(logging.WithLogger(ctx, c.Logger.Named("config-store")), resync)
	configStore.WatchConfigs(cmw)
	c.configStore = configStore

	return impl
}
