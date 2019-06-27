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

	ingressinformer "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/ingress"
	listers "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/ingress/config"
	gatewayinformer "knative.dev/pkg/client/injection/informers/istio/v1alpha3/gateway"
	virtualserviceinformer "knative.dev/pkg/client/injection/informers/istio/v1alpha3/virtualservice"
	"knative.dev/pkg/logging"

	istiolisters "knative.dev/pkg/client/listers/istio/v1alpha3"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	secretinformer "knative.dev/pkg/injection/informers/kubeinformers/corev1/secret"
	"knative.dev/pkg/tracker"

	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler"

	corev1 "k8s.io/api/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "ingress-controller"
)

// ingressFinalizer is the name that we put into the resource finalizer list, e.g.
//  metadata:
//    finalizers:
//    - ingresses.networking.internal.knative.dev
var (
	ingressResource  = v1alpha1.Resource("ingresses")
	ingressFinalizer = ingressResource.String()
)

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// Reconciler implements IngressReconciler for Ingress resources.
type Reconciler struct {
	*BaseIngressReconciler
	ingressLister listers.IngressLister
}

// BaseIngressReconciler is the conmon struct for InjectReconciles
type BaseIngressReconciler struct {
	*reconciler.Base

	// listers index properties about resources
	VirtualServiceLister istiolisters.VirtualServiceLister
	GatewayLister        istiolisters.GatewayLister
	SecretLister         corev1listers.SecretLister
	ConfigStore          reconciler.ConfigStore

	Tracker tracker.Interface
}

// NewBaseIngressReconciler creates a new BaseIngressReconciler
func NewBaseIngressReconciler(ctx context.Context, controllerAgentName string, cmw configmap.Watcher) *BaseIngressReconciler {
	virtualServiceInformer := virtualserviceinformer.Get(ctx)
	gatewayInformer := gatewayinformer.Get(ctx)
	secretInformer := secretinformer.Get(ctx)

	base := &BaseIngressReconciler{
		Base:                 reconciler.NewBase(ctx, controllerAgentName, cmw),
		VirtualServiceLister: virtualServiceInformer.Lister(),
		GatewayLister:        gatewayInformer.Lister(),
		SecretLister:         secretInformer.Lister(),
	}
	return base
}

// newInitializer creates an Ingress Reconciler and returns ReconcilerInitializer
func newInitializer(ctx context.Context, cmw configmap.Watcher) ReconcilerInitializer {
	ingressInformer := ingressinformer.Get(ctx)
	r := &Reconciler{
		BaseIngressReconciler: NewBaseIngressReconciler(ctx, controllerAgentName, cmw),
		ingressLister:         ingressInformer.Lister(),
	}
	return r
}

// SetTracker assigns the Tracker field
func (r *Reconciler) SetTracker(tracker tracker.Interface) {
	r.Tracker = tracker
}

// Init method performs initializations to ingress reconciler
func (r *Reconciler) Init(ctx context.Context, cmw configmap.Watcher, impl *controller.Impl) {

	SetupSecretTracker(ctx, cmw, r, impl)

	r.Logger.Info("Setting up Ingress event handlers")
	ingressInformer := ingressinformer.Get(ctx)

	myFilterFunc := reconciler.AnnotationFilterFunc(networking.IngressClassAnnotationKey, network.IstioIngressClassName, true)
	ingressHandler := cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    controller.HandleAll(impl.Enqueue),
	}
	ingressInformer.Informer().AddEventHandler(ingressHandler)

	virtualServiceInformer := virtualserviceinformer.Get(ctx)
	virtualServiceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	r.Logger.Info("Setting up ConfigMap receivers")
	configsToResync := []interface{}{
		&config.Istio{},
		&network.Config{},
	}
	resyncIngressesOnConfigChange := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		controller.SendGlobalUpdates(ingressInformer.Informer(), ingressHandler)
	})
	configStore := config.NewStore(r.Logger.Named("config-store"), resyncIngressesOnConfigChange)
	configStore.WatchConfigs(cmw)
	r.ConfigStore = configStore

}

// SetupSecretTracker initializes Secret Tracker
func SetupSecretTracker(ctx context.Context, cmw configmap.Watcher, init ReconcilerInitializer, impl *controller.Impl) {

	logger := logging.FromContext(ctx)
	logger.Info("Setting up secret informer event handler")

	// Create tracker
	tracker := tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))
	init.SetTracker(tracker)

	// add secret event handler
	secretInformer := secretinformer.Get(ctx)
	secretInformer.Informer().AddEventHandler(controller.HandleAll(
		controller.EnsureTypeMeta(
			tracker.OnChanged,
			corev1.SchemeGroupVersion.WithKind("Secret"),
		),
	))
}

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Ingress resource
// with the current status of the resource.
func (r *Reconciler) Reconcile(ctx context.Context, key string) error {
	// TBD
	return nil
}
