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

package nscert

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	nsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/namespace"
	namespacereconciler "knative.dev/pkg/client/injection/kube/reconciler/core/v1/namespace"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	kcertinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/certificate"
	routecfg "knative.dev/serving/pkg/reconciler/route/config"

	"knative.dev/serving/pkg/network"
	servingreconciler "knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/nscert/config"
)

const (
	controllerAgentName = "namespace-controller"
)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	ctx = servingreconciler.AnnotateLoggerWithName(ctx, controllerAgentName)
	logger := logging.FromContext(ctx)
	nsInformer := nsinformer.Get(ctx)
	knCertificateInformer := kcertinformer.Get(ctx)

	c := &reconciler{
		client:              servingclient.Get(ctx),
		knCertificateLister: knCertificateInformer.Lister(),
	}

	impl := namespacereconciler.NewImpl(ctx, c, func(impl *controller.Impl) controller.Options {
		logger.Info("Setting up event handlers")
		nsInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

		knCertificateInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
			FilterFunc: controller.FilterGroupVersionKind(
				corev1.SchemeGroupVersion.WithKind("Namespace")),
			Handler: controller.HandleAll(impl.EnqueueControllerOf),
		})

		logger.Info("Setting up ConfigMap receivers")
		configsToResync := []interface{}{
			&network.Config{},
			&routecfg.Domain{},
		}
		resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
			impl.GlobalResync(nsInformer.Informer())
		})
		configStore := config.NewStore(logger.Named("config-store"), resync)
		configStore.WatchConfigs(cmw)
		return controller.Options{ConfigStore: configStore}
	})

	return impl
}
