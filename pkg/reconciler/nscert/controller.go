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
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/serving/pkg/apis/networking"
	kcertinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/certificate"
	routecfg "knative.dev/serving/pkg/reconciler/route/config"

	"knative.dev/serving/pkg/network"
	pkgreconciler "knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/nscert/config"
)

const (
	controllerAgentName = "namespace-controller"
)

type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	nsInformer := nsinformer.Get(ctx)
	knCertificateInformer := kcertinformer.Get(ctx)

	c := &reconciler{
		Base:                pkgreconciler.NewBase(ctx, controllerAgentName, cmw),
		nsLister:            nsInformer.Lister(),
		knCertificateLister: knCertificateInformer.Lister(),
	}

	impl := controller.NewImpl(c, c.Logger, "Namespace")

	c.Logger.Info("Setting up event handlers")
	nsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: pkgreconciler.Not(pkgreconciler.LabelExistsFilterFunc(networking.DisableWildcardCertLabelKey)),
		Handler:    controller.HandleAll(impl.Enqueue),
	})

	knCertificateInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(corev1.SchemeGroupVersion.WithKind("Namespace")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	c.Logger.Info("Setting up ConfigMap receivers")
	configsToResync := []interface{}{
		&network.Config{},
		&routecfg.Domain{},
	}
	resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		impl.GlobalResync(nsInformer.Informer())
	})
	c.configStore = config.NewStore(c.Logger.Named("config-store"), resync)
	c.configStore.WatchConfigs(cmw)

	return impl
}
