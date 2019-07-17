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

package certificate

import (
	"context"

	"k8s.io/client-go/tools/cache"
	cmclient "knative.dev/serving/pkg/client/certmanager/injection/client"
	cmcertinformer "knative.dev/serving/pkg/client/certmanager/injection/informers/certmanager/v1alpha1/certificate"
	kcertinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/certificate"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/certificate/config"
)

const (
	controllerAgentName = "certificate-controller"
)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	knCertificateInformer := kcertinformer.Get(ctx)
	cmCertificateInformer := cmcertinformer.Get(ctx)

	c := &Reconciler{
		Base:                reconciler.NewBase(ctx, controllerAgentName, cmw),
		knCertificateLister: knCertificateInformer.Lister(),
		cmCertificateLister: cmCertificateInformer.Lister(),
		// TODO(mattmoor): Move this to the base.
		certManagerClient: cmclient.Get(ctx),
	}

	impl := controller.NewImpl(c, c.Logger, "Certificate")

	c.Logger.Info("Setting up event handlers")
	classFilterFunc := reconciler.AnnotationFilterFunc(networking.CertificateClassAnnotationKey, network.CertManagerCertificateClassName, true)
	certHandler := cache.FilteringResourceEventHandler{
		FilterFunc: classFilterFunc,
		Handler:    controller.HandleAll(impl.Enqueue),
	}
	knCertificateInformer.Informer().AddEventHandler(certHandler)

	cmCertificateInformer.Informer().AddEventHandler(controller.HandleAll(impl.EnqueueControllerOf))

	c.Logger.Info("Setting up ConfigMap receivers")
	resyncCertOnCertManagerconfigChange := configmap.TypeFilter(&config.CertManagerConfig{})(func(string, interface{}) {
		impl.GlobalResync(knCertificateInformer.Informer())
	})
	configStore := config.NewStore(c.Logger.Named("config-store"), resyncCertOnCertManagerconfigChange)
	configStore.WatchConfigs(cmw)
	c.configStore = configStore

	return impl
}
