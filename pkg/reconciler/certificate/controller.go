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

package certificate

import (
	"context"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	netapi "knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	kcertinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/certificate"
	certreconciler "knative.dev/networking/pkg/client/injection/reconciler/networking/v1alpha1/certificate"
	netcfg "knative.dev/networking/pkg/config"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	pkgreconciler "knative.dev/pkg/reconciler"
	cmclient "knative.dev/serving/pkg/client/certmanager/injection/client"
	cmchallengeinformer "knative.dev/serving/pkg/client/certmanager/injection/informers/acme/v1/challenge"
	cmcertinformer "knative.dev/serving/pkg/client/certmanager/injection/informers/certmanager/v1/certificate"
	clusterinformer "knative.dev/serving/pkg/client/certmanager/injection/informers/certmanager/v1/clusterissuer"
	"knative.dev/serving/pkg/reconciler/certificate/config"
)

const controllerAgentName = "certificate-controller"

// AnnotateLoggerWithName names the logger in the context with the supplied name
//
// This is a stop gap until the generated reconcilers can do this
// automatically for you
func AnnotateLoggerWithName(ctx context.Context, name string) context.Context {
	logger := logging.FromContext(ctx).
		Named(name).
		With(zap.String(logkey.ControllerType, name))

	return logging.WithLogger(ctx, logger)

}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	ctx = AnnotateLoggerWithName(ctx, controllerAgentName)
	logger := logging.FromContext(ctx)
	knCertificateInformer := kcertinformer.Get(ctx)
	cmCertificateInformer := cmcertinformer.Get(ctx)
	cmChallengeInformer := cmchallengeinformer.Get(ctx)
	clusterIssuerInformer := clusterinformer.Get(ctx)
	svcInformer := serviceinformer.Get(ctx)

	c := &Reconciler{
		cmCertificateLister: cmCertificateInformer.Lister(),
		cmChallengeLister:   cmChallengeInformer.Lister(),
		cmIssuerLister:      clusterIssuerInformer.Lister(),
		svcLister:           svcInformer.Lister(),
		certManagerClient:   cmclient.Get(ctx),
	}

	classFilterFunc := pkgreconciler.AnnotationFilterFunc(netapi.CertificateClassAnnotationKey, netcfg.CertManagerCertificateClassName, true)

	impl := certreconciler.NewImpl(ctx, c, netcfg.CertManagerCertificateClassName,
		func(impl *controller.Impl) controller.Options {
			configStore := config.NewStore(logger.Named("config-store"), configmap.TypeFilter(&config.CertManagerConfig{})(func(string, interface{}) {
				impl.FilteredGlobalResync(classFilterFunc, knCertificateInformer.Informer())
			}))
			configStore.WatchConfigs(cmw)
			return controller.Options{
				ConfigStore:       configStore,
				PromoteFilterFunc: classFilterFunc,
			}
		})

	knCertificateInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: classFilterFunc,
		Handler:    controller.HandleAll(impl.Enqueue),
	})

	cmCertificateInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterController(&v1alpha1.Certificate{}),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	c.tracker = impl.Tracker

	// Make sure trackers are deleted once the observers are removed.
	knCertificateInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: c.tracker.OnDeletedObserver,
	})

	svcInformer.Informer().AddEventHandler(controller.HandleAll(
		controller.EnsureTypeMeta(
			c.tracker.OnChanged,
			corev1.SchemeGroupVersion.WithKind("Service"),
		),
	))

	return impl
}
