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

	cmclient "github.com/knative/serving/pkg/client/certmanager/injection/client"
	cmcertinformer "github.com/knative/serving/pkg/client/certmanager/injection/informers/certmanager/v1alpha1/certificate"
	kcertinformer "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/certificate"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/certificate/config"
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
	knCertificateInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))
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
