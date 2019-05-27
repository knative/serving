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

package clusteringress

import (
	"context"

	istioinformers "github.com/knative/pkg/client/informers/externalversions/istio/v1alpha3"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/tracker"
	"github.com/knative/serving/pkg/apis/networking"
	informers "github.com/knative/serving/pkg/client/informers/externalversions/networking/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/clusteringress/config"
	corev1 "k8s.io/api/core/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "clusteringress-controller"
)

type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(
	opt reconciler.Options,
	clusterIngressInformer informers.ClusterIngressInformer,
	virtualServiceInformer istioinformers.VirtualServiceInformer,
	gatewayInformer istioinformers.GatewayInformer,
	secretInfomer corev1informers.SecretInformer,
) *controller.Impl {

	c := &Reconciler{
		Base:                 reconciler.NewBase(opt, controllerAgentName),
		clusterIngressLister: clusterIngressInformer.Lister(),
		virtualServiceLister: virtualServiceInformer.Lister(),
		gatewayLister:        gatewayInformer.Lister(),
		secretLister:         secretInfomer.Lister(),
	}
	impl := controller.NewImpl(c, c.Logger, "ClusterIngresses", reconciler.MustNewStatsReporter("ClusterIngress", c.Logger))

	c.Logger.Info("Setting up event handlers")
	myFilterFunc := reconciler.AnnotationFilterFunc(networking.IngressClassAnnotationKey, network.IstioIngressClassName, true)
	ciHandler := cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    reconciler.Handler(impl.Enqueue),
	}
	clusterIngressInformer.Informer().AddEventHandler(ciHandler)

	virtualServiceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    reconciler.Handler(impl.EnqueueLabelOfClusterScopedResource(networking.IngressLabelKey)),
	})

	c.tracker = tracker.New(impl.EnqueueKey, opt.GetTrackerLease())
	secretInfomer.Informer().AddEventHandler(reconciler.Handler(
		controller.EnsureTypeMeta(
			c.tracker.OnChanged,
			corev1.SchemeGroupVersion.WithKind("Secret"),
		),
	))

	c.Logger.Info("Setting up ConfigMap receivers")
	configsToResync := []interface{}{
		&config.Istio{},
		&network.Config{},
	}
	resyncIngressesOnConfigChange := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		controller.SendGlobalUpdates(clusterIngressInformer.Informer(), ciHandler)
	})
	c.configStore = config.NewStore(c.Logger.Named("config-store"), resyncIngressesOnConfigChange)
	c.configStore.WatchConfigs(opt.ConfigMapWatcher)
	return impl
}
