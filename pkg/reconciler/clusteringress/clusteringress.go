/*
Copyright 2018 The Knative Authors

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
	"encoding/json"
	"knative.dev/pkg/apis/istio/v1alpha3"
	"log"

	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler"

	virtualserviceinformer "knative.dev/pkg/client/injection/informers/istio/v1alpha3/virtualservice"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/tracker"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	clusteringressinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/clusteringress"
	listers "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	ing "knative.dev/serving/pkg/reconciler/ingress"
	"knative.dev/serving/pkg/reconciler/ingress/config"

	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "clusteringress-controller"
)

// Reconciler implements controller.Reconciler for ClusterIngress resources.
type Reconciler struct {
	*ing.BaseIngressReconciler
	clusterIngressLister listers.ClusterIngressLister
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// clusterIngressFinalizer is the name that we put into the resource finalizer list, e.g.
//  metadata:
//    finalizers:
//    - clusteringresses.networking.internal.knative.dev
var (
	clusterIngressResource  = v1alpha1.Resource("clusteringresses")
	clusterIngressFinalizer = clusterIngressResource.String()
)

// newInitializer creates an Ingress Reconciler and returns ReconcilerInitializer
func newInitializer(ctx context.Context, cmw configmap.Watcher) ing.ReconcilerInitializer {
	clusterIngressInformer := clusteringressinformer.Get(ctx)
	r := &Reconciler{
		BaseIngressReconciler: ing.NewBaseIngressReconciler(ctx, controllerAgentName, clusterIngressFinalizer, cmw),
		clusterIngressLister:  clusterIngressInformer.Lister(),
	}
	return r
}

// SetTracker assigns the Tracker field
func (c *Reconciler) SetTracker(tracker tracker.Interface) {
	c.Tracker = tracker
}

// Init method performs initializations to ingress reconciler
func (c *Reconciler) Init(ctx context.Context, cmw configmap.Watcher, impl *controller.Impl) {

	ing.SetupSecretTracker(ctx, cmw, c, impl)

	c.Logger.Info("Setting up Ingress event handlers")
	clusterIngressInformer := clusteringressinformer.Get(ctx)

	myFilterFunc := reconciler.AnnotationFilterFunc(networking.IngressClassAnnotationKey, network.IstioIngressClassName, true)
	clusterIngressHandler := cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    controller.HandleAll(impl.Enqueue),
	}
	clusterIngressInformer.Informer().AddEventHandler(clusterIngressHandler)

	virtualServiceInformer := virtualserviceinformer.Get(ctx)
	virtualServiceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    controller.HandleAll(impl.EnqueueLabelOfClusterScopedResource(networking.ClusterIngressLabelKey)),
	})
	virtualServiceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			vs, _ := obj.(*v1alpha3.VirtualService)
			c.Logger.Infof("VirtualService - Add: %s/%s: %s", vs.Namespace, vs.Name, toJSON(vs))
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldVs, _ := oldObj.(*v1alpha3.VirtualService)
			newVs, _ := newObj.(*v1alpha3.VirtualService)
			c.Logger.Infof("VirtualService - Update: %s/%s, Old: %s, New: %s", oldVs.Namespace, oldVs.Name, toJSON(oldVs), toJSON(newVs))
		},
		DeleteFunc: func(obj interface{}) {
			vs, _ := obj.(*v1alpha3.VirtualService)
			c.Logger.Infof("VirtualService - Delete: %s/%s: %s", vs.Namespace, vs.Name, toJSON(vs))
		},
	})

	c.Logger.Info("Setting up ConfigMap receivers")
	configsToResync := []interface{}{
		&config.Istio{},
		&network.Config{},
	}
	resyncIngressesOnConfigChange := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		c.Logger.Infof("Reconcile all Ingresses because of ConfigMap changes")
		controller.SendGlobalUpdates(clusterIngressInformer.Informer(), clusterIngressHandler)
	})
	configStore := config.NewStore(c.Logger.Named("config-store"), resyncIngressesOnConfigChange)
	configStore.WatchConfigs(cmw)
	c.BaseIngressReconciler.ConfigStore = configStore

}

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the ClusterIngress resource
// with the current status of the resource.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	return c.BaseIngressReconciler.ReconcileIngress(c.ConfigStore.ToContext(ctx), c, key)
}
func toJSON(obj interface{}) string {
	bytes, err := json.Marshal(obj)
	if err != nil {
		log.Fatalf("failed to serialize to JSON: %v", err)
	}
	return string(bytes)
}
