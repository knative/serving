package kncertreconciler

import (
	"context"

	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	"knative.dev/networking/pkg/apis/config"
	"knative.dev/networking/pkg/certificates"
	"knative.dev/pkg/client/injection/kube/client"
	secretfilteredinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/secret/filtered"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"

	netapi "knative.dev/networking/pkg/apis/networking"
	kcertinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/certificate"
	certreconciler "knative.dev/networking/pkg/client/injection/reconciler/networking/v1alpha1/certificate"
	netcfg "knative.dev/networking/pkg/config"
	filteredFactory "knative.dev/pkg/client/injection/kube/informers/factory/filtered"
)

// NewControllerFactory generates a ControllerConstructor for the Knative certificate issuer controller
// and registers event handlers to enqueue events.
func NewControllerFactory(componentName string) injection.ControllerConstructor {
	return func(ctx context.Context,
		cmw configmap.Watcher,
	) *controller.Impl {
		logger := logging.FromContext(ctx)
		knCertificateInformer := kcertinformer.Get(ctx)

		caSecretName := componentName + certificates.KnativeCertIssuerCASecretNamePostfix
		labelName := componentName + certificates.KnativeCertIssuerSecretLabelPostfix

		ctx = filteredFactory.WithSelectors(ctx, labelName)
		secretInformer := getSecretInformer(ctx)

		c := &reconciler{
			client:       client.Get(ctx),
			secretLister: secretInformer.Lister(),
			caSecretName: caSecretName,
			labelName:    labelName,
		}

		classFilterFunc := pkgreconciler.AnnotationFilterFunc(netapi.CertificateClassAnnotationKey, netcfg.KnativeSelfSignedCertificateClassName, true)

		impl := certreconciler.NewImpl(ctx, c, netcfg.KnativeSelfSignedCertificateClassName,
			func(impl *controller.Impl) controller.Options {
				// Trigger full re-sync on config-network CM change
				configsToResync := []interface{}{
					&netcfg.Config{},
				}
				resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
					impl.FilteredGlobalResync(classFilterFunc, knCertificateInformer.Informer())
				})
				configStore := config.NewStore(logger.Named("config-store"), resync)
				configStore.WatchConfigs(cmw)
				return controller.Options{ConfigStore: configStore}
			})

		c.enqueueAfter = impl.EnqueueKeyAfter

		// If the CA secret changes, global resync
		secretInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
			FilterFunc: controller.FilterWithNameAndNamespace(system.Namespace(), caSecretName),
			Handler: controller.HandleAll(func(i interface{}) {
				impl.FilteredGlobalResync(classFilterFunc, knCertificateInformer.Informer())
			}),
		})

		// Handle all KnativeCertificate changes that match our certificate-class.
		knCertificateInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
			FilterFunc: classFilterFunc,
			Handler:    controller.HandleAll(impl.Enqueue),
		})

		return impl
	}
}

func getSecretInformer(ctx context.Context) v1.SecretInformer {
	untyped := ctx.Value(filteredFactory.LabelKey{}) // This should always be not nil and have exactly one selector
	return secretfilteredinformer.Get(ctx, untyped.([]string)[0])
}
