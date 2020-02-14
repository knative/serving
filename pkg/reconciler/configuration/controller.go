/*
Copyright 2019 The Knative Authors.

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

package configuration

import (
	"context"

	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	configurationinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	configreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/configuration"
	"knative.dev/serving/pkg/reconciler"
)

const controllerAgentName = "configuration-controller"

// NewController creates a new Configuration controller
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {

	configurationInformer := configurationinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)

	c := &Reconciler{
		Base:           reconciler.NewBase(ctx, controllerAgentName, cmw),
		revisionLister: revisionInformer.Lister(),
	}
	impl := configreconciler.NewImpl(ctx, c)

	c.Logger.Info("Setting up event handlers")
	configurationInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	revisionInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterGroupKind(v1.Kind("Configuration")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	return impl
}
