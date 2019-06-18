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

package ingress

import (
	"context"

	gatewayinformer "github.com/knative/pkg/client/injection/informers/istio/v1alpha3/gateway"
	virtualserviceinformer "github.com/knative/pkg/client/injection/informers/istio/v1alpha3/virtualservice"
	ingressinformer "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/ingress"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	secretinformer "github.com/knative/pkg/injection/informers/kubeinformers/corev1/secret"
	"github.com/knative/pkg/tracker"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/ingress/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "ingress-controller"
)

type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {

	ingressInformer := ingressinformer.Get(ctx)
	virtualServiceInformer := virtualserviceinformer.Get(ctx)
	gatewayInformer := gatewayinformer.Get(ctx)
	secretInformer := secretinformer.Get(ctx)

	c := &Reconciler{
		Base:                 reconciler.NewBase(ctx, controllerAgentName, cmw),
		ingressLister:        ingressInformer.Lister(),
		virtualServiceLister: virtualServiceInformer.Lister(),
		gatewayLister:        gatewayInformer.Lister(),
		secretLister:         secretInformer.Lister(),
	}
	impl := controller.NewImpl(c, c.Logger, "Ingresses")

	c.Logger.Info("Setting up event handlers")
	myFilterFunc := reconciler.AnnotationFilterFunc(networking.IngressClassAnnotationKey, network.IstioIngressClassName, true)
	ciHandler := cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    controller.HandleAll(impl.Enqueue),
	}
	ingressInformer.Informer().AddEventHandler(ciHandler)

	virtualServiceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler: controller.HandleAll(impl.EnqueueLabelOfNamespaceScopedResource(networking.IngressNamespaceLabelKey,
			networking.IngressLabelKey)),
	})

	c.tracker = tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))

	secretInformer.Informer().AddEventHandler(controller.HandleAll(
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
		controller.SendGlobalUpdates(ingressInformer.Informer(), ciHandler)
	})
	configStore := config.NewStore(c.Logger.Named("config-store"), "ingress", resyncIngressesOnConfigChange)
	configStore.WatchConfigs(cmw)
	c.configStore = configStore

	return impl
}
