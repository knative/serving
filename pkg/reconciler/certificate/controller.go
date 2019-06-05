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

	certmanagerclientset "github.com/knative/serving/pkg/client/certmanager/clientset/versioned"
	certmanagerinformers "github.com/knative/serving/pkg/client/certmanager/informers/externalversions/certmanager/v1alpha1"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	informers "github.com/knative/serving/pkg/client/informers/externalversions/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/certificate/config"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "certificate-controller"
)

type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(
	opt reconciler.Options,
	knCertificateInformer informers.CertificateInformer,
	cmCertificateInformer certmanagerinformers.CertificateInformer,
	certManagerClient certmanagerclientset.Interface,
) *controller.Impl {
	c := &Reconciler{
		Base:                reconciler.NewBase(opt, controllerAgentName),
		knCertificateLister: knCertificateInformer.Lister(),
		cmCertificateLister: cmCertificateInformer.Lister(),
		certManagerClient:   certManagerClient,
	}

	impl := controller.NewImpl(c, c.Logger, "Certificate")

	c.Logger.Info("Setting up event handlers")
	knCertificateInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.Enqueue,
		UpdateFunc: controller.PassNew(impl.Enqueue),
		DeleteFunc: impl.Enqueue,
	})

	cmCertificateInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.EnqueueControllerOf,
		UpdateFunc: controller.PassNew(impl.EnqueueControllerOf),
		DeleteFunc: impl.EnqueueControllerOf,
	})

	c.Logger.Info("Setting up ConfigMap receivers")
	resyncCertOnCertManagerconfigChange := configmap.TypeFilter(&config.CertManagerConfig{})(func(string, interface{}) {
		impl.GlobalResync(knCertificateInformer.Informer())
	})
	c.configStore = config.NewStore(c.Logger.Named("config-store"), resyncCertOnCertManagerconfigChange)
	c.configStore.WatchConfigs(opt.ConfigMapWatcher)

	return impl
}
