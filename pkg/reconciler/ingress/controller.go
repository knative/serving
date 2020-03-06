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

	kubeclient "knative.dev/pkg/client/injection/kube/client"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	podinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
	secretinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/secret"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/tracker"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	ingressinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/ingress"
	ingressreconciler "knative.dev/serving/pkg/client/injection/reconciler/networking/v1alpha1/ingress"
	istioclient "knative.dev/serving/pkg/client/istio/injection/client"
	gatewayinformer "knative.dev/serving/pkg/client/istio/injection/informers/networking/v1alpha3/gateway"
	virtualserviceinformer "knative.dev/serving/pkg/client/istio/injection/informers/networking/v1alpha3/virtualservice"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/network/status"
	servingreconciler "knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/ingress/config"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
)

const controllerAgentName = "istio-ingress-controller"

type ingressOption func(*Reconciler)

// NewController works as a constructor for Ingress Controller
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	return newControllerWithOptions(ctx, cmw)
}

func newControllerWithOptions(
	ctx context.Context,
	cmw configmap.Watcher,
	opts ...ingressOption,
) *controller.Impl {

	ctx = servingreconciler.AnnotateLoggerWithName(ctx, controllerAgentName)
	logger := logging.FromContext(ctx)
	virtualServiceInformer := virtualserviceinformer.Get(ctx)
	gatewayInformer := gatewayinformer.Get(ctx)
	secretInformer := secretinformer.Get(ctx)
	ingressInformer := ingressinformer.Get(ctx)

	c := &Reconciler{
		kubeclient:           kubeclient.Get(ctx),
		istioClientSet:       istioclient.Get(ctx),
		virtualServiceLister: virtualServiceInformer.Lister(),
		gatewayLister:        gatewayInformer.Lister(),
		secretLister:         secretInformer.Lister(),
		finalizer:            ingressFinalizer,
	}
	myFilterFunc := reconciler.AnnotationFilterFunc(networking.IngressClassAnnotationKey, network.IstioIngressClassName, true)

	impl := ingressreconciler.NewImpl(ctx, c, func(impl *controller.Impl) controller.Options {
		logger.Info("Setting up ConfigMap receivers")
		configsToResync := []interface{}{
			&config.Istio{},
			&network.Config{},
		}
		resyncIngressesOnConfigChange := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
			impl.FilteredGlobalResync(myFilterFunc, ingressInformer.Informer())
		})
		configStore := config.NewStore(logger.Named("config-store"), resyncIngressesOnConfigChange)
		configStore.WatchConfigs(cmw)
		return controller.Options{ConfigStore: configStore}
	})

	logger.Info("Setting up Ingress event handlers")
	ingressHandler := cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    controller.HandleAll(impl.Enqueue),
	}
	ingressInformer.Informer().AddEventHandler(ingressHandler)

	virtualServiceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	logger.Info("Setting up statusManager")
	endpointsInformer := endpointsinformer.Get(ctx)
	serviceInformer := serviceinformer.Get(ctx)
	podInformer := podinformer.Get(ctx)
	resyncOnIngressReady := func(ing *v1alpha1.Ingress) {
		impl.EnqueueKey(types.NamespacedName{Namespace: ing.GetNamespace(), Name: ing.GetName()})
	}
	statusProber := status.NewProber(
		logger.Named("status-manager"),
		NewProbeTargetLister(
			logger.Named("probe-lister"),
			gatewayInformer.Lister(),
			endpointsInformer.Lister(),
			serviceInformer.Lister()),
		resyncOnIngressReady)
	c.statusManager = statusProber
	statusProber.Start(ctx.Done())

	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// Cancel probing when a Ingress is deleted
		DeleteFunc: statusProber.CancelIngressProbing,
	})
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// Cancel probing when a Pod is deleted
		DeleteFunc: statusProber.CancelPodProbing,
	})

	logger.Info("Setting up secret informer event handler")
	tracker := tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))
	c.tracker = tracker

	secretInformer.Informer().AddEventHandler(controller.HandleAll(
		controller.EnsureTypeMeta(
			tracker.OnChanged,
			corev1.SchemeGroupVersion.WithKind("Secret"),
		),
	))
	for _, opt := range opts {
		opt(c)
	}
	return impl
}
