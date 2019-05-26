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

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/system"
	"github.com/knative/pkg/tracker"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	networkinginformers "github.com/knative/serving/pkg/client/informers/externalversions/networking/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/route/config"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "route-controller"
)

type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
func NewController(
	opt reconciler.Options,
	routeInformer servinginformers.RouteInformer,
	configInformer servinginformers.ConfigurationInformer,
	revisionInformer servinginformers.RevisionInformer,
	serviceInformer corev1informers.ServiceInformer,
	clusterIngressInformer networkinginformers.ClusterIngressInformer,
	certificateInformer networkinginformers.CertificateInformer,
) *controller.Impl {
	return NewControllerWithClock(opt, routeInformer, configInformer, revisionInformer,
		serviceInformer, clusterIngressInformer, certificateInformer, system.RealClock{})
}

func NewControllerWithClock(
	opt reconciler.Options,
	routeInformer servinginformers.RouteInformer,
	configInformer servinginformers.ConfigurationInformer,
	revisionInformer servinginformers.RevisionInformer,
	serviceInformer corev1informers.ServiceInformer,
	clusterIngressInformer networkinginformers.ClusterIngressInformer,
	certificateInformer networkinginformers.CertificateInformer,
	clock system.Clock,
) *controller.Impl {

	// No need to lock domainConfigMutex yet since the informers that can modify
	// domainConfig haven't started yet.
	c := &Reconciler{
		Base:                 reconciler.NewBase(opt, controllerAgentName),
		routeLister:          routeInformer.Lister(),
		configurationLister:  configInformer.Lister(),
		revisionLister:       revisionInformer.Lister(),
		serviceLister:        serviceInformer.Lister(),
		clusterIngressLister: clusterIngressInformer.Lister(),
		certificateLister:    certificateInformer.Lister(),
		clock:                clock,
	}
	impl := controller.NewImpl(c, c.Logger, "Routes", reconciler.MustNewStatsReporter("Routes", c.Logger))

	c.Logger.Info("Setting up event handlers")
	routeInformer.Informer().AddEventHandler(reconciler.Handler(impl.Enqueue))

	serviceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Route")),
		Handler:    reconciler.Handler(impl.EnqueueControllerOf),
	})

	clusterIngressInformer.Informer().AddEventHandler(reconciler.Handler(
		impl.EnqueueLabelOfNamespaceScopedResource(
			serving.RouteNamespaceLabelKey, serving.RouteLabelKey)))

	c.tracker = tracker.New(impl.EnqueueKey, opt.GetTrackerLease())

	configInformer.Informer().AddEventHandler(reconciler.Handler(
		// Call the tracker's OnChanged method, but we've seen the objects
		// coming through this path missing TypeMeta, so ensure it is properly
		// populated.
		controller.EnsureTypeMeta(
			c.tracker.OnChanged,
			v1alpha1.SchemeGroupVersion.WithKind("Configuration"),
		),
	))

	revisionInformer.Informer().AddEventHandler(reconciler.Handler(
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
		Handler:    reconciler.Handler(impl.EnqueueControllerOf),
	})

	c.Logger.Info("Setting up ConfigMap receivers")
	configsToResync := []interface{}{
		&network.Config{},
		&config.Domain{},
	}
	resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		impl.GlobalResync(routeInformer.Informer())
	})
	c.configStore = config.NewStore(c.Logger.Named("config-store"), resync)
	c.configStore.WatchConfigs(opt.ConfigMapWatcher)
	return impl
}
