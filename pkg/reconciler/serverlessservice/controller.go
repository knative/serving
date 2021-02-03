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

package serverlessservice

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"

	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	sksinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/serverlessservice"
	sksreconciler "knative.dev/networking/pkg/client/injection/reconciler/networking/v1alpha1/serverlessservice"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/client/injection/ducks/autoscaling/v1alpha1/podscalable"
	"knative.dev/serving/pkg/networking"
)

// NewController initializes the controller and is called by the generated code.
// Registers eventhandlers to enqueue events.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {

	logger := logging.FromContext(ctx)
	serviceInformer := serviceinformer.Get(ctx)
	endpointsInformer := endpointsinformer.Get(ctx)
	psInformerFactory := podscalable.Get(ctx)
	sksInformer := sksinformer.Get(ctx)

	listerCache := make(map[schema.GroupVersionResource]cache.GenericLister, 1)
	var mu sync.RWMutex
	c := &reconciler{
		kubeclient: kubeclient.Get(ctx),

		endpointsLister: endpointsInformer.Lister(),
		serviceLister:   serviceInformer.Lister(),

		// We wrap the PodScalable Informer Factory here so Get() uses the outer context.
		// As the returned Informer is shared across reconciles, passing the context from
		// an individual reconcile to Get() could lead to problems.
		listerFactory: func(gvr schema.GroupVersionResource) (cache.GenericLister, error) {
			l := func() cache.GenericLister {
				mu.RLock()
				defer mu.RUnlock()
				return listerCache[gvr]
			}()
			if l != nil {
				return l, nil
			}

			_, l, err := psInformerFactory.Get(ctx, gvr)
			if l != nil && err == nil {
				mu.Lock()
				defer mu.Unlock()
				listerCache[gvr] = l
			}
			return l, err
		},
	}
	impl := sksreconciler.NewImpl(ctx, c)

	logger.Info("Setting up event handlers")

	// Watch all the SKS objects.
	sksInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	// Watch all the endpoints that we have attached our label to.
	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: pkgreconciler.LabelExistsFilterFunc(networking.SKSLabelKey),
		Handler:    controller.HandleAll(impl.EnqueueLabelOfNamespaceScopedResource("" /*any namespace*/, networking.SKSLabelKey)),
	})

	// Watch all the services that we have created.
	serviceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterControllerGVK(netv1alpha1.SchemeGroupVersion.WithKind("ServerlessService")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	// Watch activator-service endpoints.
	grCb := func(obj interface{}) {
		// Since changes in the Activator Service endpoints affect all the SKS objects,
		// do a global resync.
		logger.Info("Doing a global resync due to activator endpoint changes")
		impl.GlobalResync(sksInformer.Informer())
	}
	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		// Accept only ActivatorService K8s service objects.
		FilterFunc: pkgreconciler.ChainFilterFuncs(
			pkgreconciler.NamespaceFilterFunc(system.Namespace()),
			pkgreconciler.NameFilterFunc(networking.ActivatorServiceName)),
		Handler: controller.HandleAll(grCb),
	})

	return impl
}
