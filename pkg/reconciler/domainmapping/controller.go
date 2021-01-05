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
	network "knative.dev/networking/pkg"
	netclient "knative.dev/networking/pkg/client/injection/client"
	certificateinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/certificate"
	ingressinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/ingress"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/resolver"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/domainmapping"
	kindreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1alpha1/domainmapping"
	"knative.dev/serving/pkg/reconciler/domainmapping/config"
)

// NewController creates a new DomainMapping controller.
func NewController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	logger := logging.FromContext(ctx)
	certificateInformer := certificateinformer.Get(ctx)
	domainmappingInformer := domainmapping.Get(ctx)
	ingressInformer := ingressinformer.Get(ctx)

	r := &Reconciler{
		certificateLister: certificateInformer.Lister(),
		ingressLister:     ingressInformer.Lister(),
		netclient:         netclient.Get(ctx),
	}

	impl := kindreconciler.NewImpl(ctx, r, func(impl *controller.Impl) controller.Options {
		configsToResync := []interface{}{
			&network.Config{},
		}
		resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
			impl.GlobalResync(domainmappingInformer.Informer())
		})
		configStore := config.NewStore(logging.WithLogger(ctx, logger.Named("config-store")), resync)
		configStore.WatchConfigs(cmw)
		return controller.Options{ConfigStore: configStore}
	})

	logger.Info("Setting up event handlers")
	domainmappingInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	handleControllerOf := cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterControllerGK(v1alpha1.Kind("DomainMapping")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	}
	certificateInformer.Informer().AddEventHandler(handleControllerOf)
	ingressInformer.Informer().AddEventHandler(handleControllerOf)

	r.resolver = resolver.NewURIResolver(ctx, impl.EnqueueKey)

	return impl
}
