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

	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	podinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
	secretinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/secret"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/tracker"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	ingressinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/ingress"
	gatewayinformer "knative.dev/serving/pkg/client/istio/injection/informers/networking/v1alpha3/gateway"
	virtualserviceinformer "knative.dev/serving/pkg/client/istio/injection/informers/networking/v1alpha3/virtualservice"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/network/status"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/ingress/config"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "ingress-controller"
)

// NewController works as a constructor for Ingress Controller
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	virtualServiceInformer := virtualserviceinformer.Get(ctx)
	gatewayInformer := gatewayinformer.Get(ctx)
	secretInformer := secretinformer.Get(ctx)
	ingressInformer := ingressinformer.Get(ctx)

	c := &Reconciler{
		Base:                 reconciler.NewBase(ctx, controllerAgentName, cmw),
		virtualServiceLister: virtualServiceInformer.Lister(),
		gatewayLister:        gatewayInformer.Lister(),
		secretLister:         secretInformer.Lister(),
		ingressLister:        ingressInformer.Lister(),
		finalizer:            ingressFinalizer,
	}
	impl := controller.NewImpl(c, c.Logger, "Ingresses")

	c.Logger.Info("Setting up Ingress event handlers")
	myFilterFunc := reconciler.AnnotationFilterFunc(networking.IngressClassAnnotationKey, network.IstioIngressClassName, true)
	ingressHandler := cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    controller.HandleAll(impl.Enqueue),
	}
	ingressInformer.Informer().AddEventHandler(ingressHandler)

	virtualServiceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	c.Logger.Info("Setting up ConfigMap receivers")
	configsToResync := []interface{}{
		&config.Istio{},
		&network.Config{},
	}
	resyncIngressesOnConfigChange := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		impl.FilteredGlobalResync(myFilterFunc, ingressInformer.Informer())
	})
	configStore := config.NewStore(c.Logger.Named("config-store"), resyncIngressesOnConfigChange)
	configStore.WatchConfigs(cmw)
	c.configStore = configStore

	c.Logger.Info("Setting up statusManager")
	endpointsInformer := endpointsinformer.Get(ctx)
	serviceInformer := serviceinformer.Get(ctx)
	podInformer := podinformer.Get(ctx)
	resyncOnIngressReady := func(ing *v1alpha1.Ingress) {
		impl.EnqueueKey(types.NamespacedName{Namespace: ing.GetNamespace(), Name: ing.GetName()})
	}
	statusProber := status.NewProber(
		c.Logger.Named("status-manager"),
		NewProbeTargetLister(
			c.Logger.Named("probe-lister"),
			gatewayInformer.Lister(),
			endpointsInformer.Lister(),
			serviceInformer.Lister()),
		resyncOnIngressReady)
	c.statusManager = statusProber
	statusProber.Start(ctx.Done())

	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// Cancel probing when a VirtualService is deleted
		DeleteFunc: statusProber.CancelIngressProbing,
	})
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// Cancel probing when a Pod is deleted
		DeleteFunc: statusProber.CancelPodProbing,
	})

	c.Logger.Info("Setting up secret informer event handler")
	tracker := tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))
	c.tracker = tracker

	secretInformer.Informer().AddEventHandler(controller.HandleAll(
		controller.EnsureTypeMeta(
			tracker.OnChanged,
			corev1.SchemeGroupVersion.WithKind("Secret"),
		),
	))

	return impl
}
