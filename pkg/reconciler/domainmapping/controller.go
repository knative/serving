/*
Copyright 2020 The Knative Authors

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

package domainmapping

import (
	"context"

	"k8s.io/client-go/tools/cache"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	netclient "knative.dev/networking/pkg/client/injection/client"
	certificateinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/certificate"
	domainclaiminformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/clusterdomainclaim"
	ingressinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/ingress"
	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/resolver"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	routeinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/route"
	serviceinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/service"
	"knative.dev/serving/pkg/client/injection/informers/serving/v1beta1/domainmapping"
	kindreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1beta1/domainmapping"
	"knative.dev/serving/pkg/reconciler/domainmapping/config"
)

// NewController creates a new DomainMapping controller.
func NewController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	logger := logging.FromContext(ctx)
	certificateInformer := certificateinformer.Get(ctx)
	domainmappingInformer := domainmapping.Get(ctx)
	ingressInformer := ingressinformer.Get(ctx)
	domainClaimInformer := domainclaiminformer.Get(ctx)
	routeInformer := routeinformer.Get(ctx)
	serviceInformer := serviceinformer.Get(ctx)

	r := &Reconciler{
		certificateLister: certificateInformer.Lister(),
		ingressLister:     ingressInformer.Lister(),
		domainClaimLister: domainClaimInformer.Lister(),
		routeLister:       routeInformer.Lister(),
		serviceLister:     serviceInformer.Lister(),
		netclient:         netclient.Get(ctx),
	}

	impl := kindreconciler.NewImpl(ctx, r, func(impl *controller.Impl) controller.Options {
		configsToResync := []interface{}{
			&netcfg.Config{},
		}
		resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
			impl.GlobalResync(domainmappingInformer.Informer())
		})
		configStore := config.NewStore(logging.WithLogger(ctx, logger.Named("config-store")), resync)
		configStore.WatchConfigs(cmw)
		return controller.Options{ConfigStore: configStore}
	})

	domainmappingInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	handleControllerOf := cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterController(&v1beta1.DomainMapping{}),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	}
	certificateInformer.Informer().AddEventHandler(handleControllerOf)
	ingressInformer.Informer().AddEventHandler(handleControllerOf)

	r.tracker = impl.Tracker
	r.resolver = resolver.NewURIResolverFromTracker(ctx, r.tracker)

	// Track Route and Ingress changes because their HTTP paths are copied into
	// DomainMapping Ingresses. The resolver already tracks referenced Services.
	// Informer events may omit TypeMeta, so populate it before notifying the tracker.
	routeInformer.Informer().AddEventHandler(controller.HandleAll(
		controller.EnsureTypeMeta(r.tracker.OnChanged, servingv1.SchemeGroupVersion.WithKind("Route")),
	))
	ingressInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterController(&servingv1.Route{}),
		Handler: controller.HandleAll(
			controller.EnsureTypeMeta(r.tracker.OnChanged, netv1alpha1.SchemeGroupVersion.WithKind("Ingress")),
		),
	})
	domainmappingInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: r.tracker.OnDeletedObserver,
	})

	return impl
}
