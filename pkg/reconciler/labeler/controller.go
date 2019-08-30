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

package labeler

import (
	"context"

	configurationinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/configuration"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision"
	routeinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/route"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/serving/pkg/reconciler"
)

const (
	controllerAgentName = "labeler-controller"
)

// NewController wraps a new instance of the labeler that labels
// Configurations with Routes in a controller.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {

	routeInformer := routeinformer.Get(ctx)
	configInformer := configurationinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)

	c := &Reconciler{
		Base:                reconciler.NewBase(ctx, controllerAgentName, cmw),
		routeLister:         routeInformer.Lister(),
		configurationLister: configInformer.Lister(),
		revisionLister:      revisionInformer.Lister(),
	}
	impl := controller.NewImpl(c, c.Logger, "Labels")

	c.Logger.Info("Setting up event handlers")
	routeInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	return impl
}
