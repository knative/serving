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

	serviceinformer "github.com/knative/pkg/injection/informers/kubeinformers/corev1/service"
	certificateinformer "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/certificate"
	clusteringressinformer "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/clusteringress"
	configurationinformer "github.com/knative/serving/pkg/client/injection/informers/serving/v1alpha1/configuration"
	revisioninformer "github.com/knative/serving/pkg/client/injection/informers/serving/v1alpha1/revision"
	routeinformer "github.com/knative/serving/pkg/client/injection/informers/serving/v1alpha1/route"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/system"
	"github.com/knative/pkg/tracker"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/route/config"
	"k8s.io/client-go/tools/cache"
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
	clusterIngressInformer := clusteringressinformer.Get(ctx)
	certificateInformer := certificateinformer.Get(ctx)

	// No need to lock domainConfigMutex yet since the informers that can modify
	// domainConfig haven't started yet.
	c := &Reconciler{
		Base:                 reconciler.NewBase(ctx, controllerAgentName, cmw),
		routeLister:          routeInformer.Lister(),
		configurationLister:  configInformer.Lister(),
		revisionLister:       revisionInformer.Lister(),
		serviceLister:        serviceInformer.Lister(),
		clusterIngressLister: clusterIngressInformer.Lister(),
		certificateLister:    certificateInformer.Lister(),
		clock:                clock,
	}
	impl := controller.NewImpl(c, c.Logger, "Routes")

	c.Logger.Info("Setting up event handlers")
	routeInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	serviceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Route")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	clusterIngressInformer.Informer().AddEventHandler(controller.HandleAll(
		impl.EnqueueLabelOfNamespaceScopedResource(
			serving.RouteNamespaceLabelKey, serving.RouteLabelKey)))

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
	configStore := config.NewStore(c.Logger.Named("config-store"), controller.GetResyncPeriod(ctx), resync)
	configStore.WatchConfigs(cmw)
	c.configStore = configStore

	return impl
}
