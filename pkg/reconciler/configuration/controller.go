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

package configuration

import (
	"context"

	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	configurationinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	configreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/configuration"
	"knative.dev/serving/pkg/reconciler/configuration/config"
)

// NewController creates a new Configuration controller
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	logger := logging.FromContext(ctx)
	configurationInformer := configurationinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)

	logger.Info("Setting up ConfigMap receivers")
	configStore := config.NewStore(logger.Named("config-store"))
	configStore.WatchConfigs(cmw)

	c := &Reconciler{
		client:         servingclient.Get(ctx),
		revisionLister: revisionInformer.Lister(),
		clock:          &clock.RealClock{},
	}
	impl := configreconciler.NewImpl(ctx, c, func(*controller.Impl) controller.Options {
		return controller.Options{ConfigStore: configStore}
	})

	logger.Info("Setting up event handlers")
	configurationInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	revisionInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterController(&v1.Configuration{}),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	return impl
}
