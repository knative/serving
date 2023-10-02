/*
Copyright 2021 The Knative Authors

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

package reconciler

import (
	"context"

	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/system"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"

	pkgreconciler "knative.dev/pkg/reconciler"

	secretfilteredinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/secret/filtered"
	filteredFactory "knative.dev/pkg/client/injection/kube/informers/factory/filtered"
)

const (
	caSecretNamePostfix    = "-ctrl-ca"
	secretLabelNamePostfix = "-ctrl"
)

// NewControllerFactory generates a ControllerConstructor for the control certificates reconciler.
func NewControllerFactory(componentName string) injection.ControllerConstructor {
	return func(
		ctx context.Context,
		cmw configmap.Watcher,
	) *controller.Impl {

		caSecretName := componentName + caSecretNamePostfix
		labelName := componentName + secretLabelNamePostfix

		ctx = filteredFactory.WithSelectors(ctx, labelName)
		secretInformer := getSecretInformer(ctx)

		r := &reconciler{
			client: kubeclient.Get(ctx),

			secretLister:        secretInformer.Lister(),
			caSecretName:        caSecretName,
			secretTypeLabelName: labelName,

			logger: logging.FromContext(ctx),
		}

		impl := NewFilteredImpl(ctx, r, secretInformer)
		r.enqueueAfter = impl.EnqueueKeyAfter

		logging.FromContext(ctx).Info("Setting up event handlers")

		// If the ca secret changes, global resync
		secretInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
			FilterFunc: controller.FilterWithNameAndNamespace(system.Namespace(), caSecretName),
			Handler: controller.HandleAll(func(i interface{}) {
				impl.FilteredGlobalResync(pkgreconciler.LabelExistsFilterFunc(labelName), secretInformer.Informer())
			}),
		})

		// Enqueue only secrets with expected label
		secretInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
			FilterFunc: pkgreconciler.LabelExistsFilterFunc(labelName),
			Handler:    controller.HandleAll(impl.Enqueue),
		})

		return impl
	}
}

func getSecretInformer(ctx context.Context) v1.SecretInformer {
	untyped := ctx.Value(filteredFactory.LabelKey{}) // This should always be not nil and have exactly one selector
	return secretfilteredinformer.Get(ctx, untyped.([]string)[0])
}
