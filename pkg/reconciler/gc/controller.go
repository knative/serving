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

package gc

import (
	"context"

	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	configurationinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	configreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/configuration"
	gcconfig "knative.dev/serving/pkg/gc"
	configns "knative.dev/serving/pkg/reconciler/gc/config"
)

const controllerAgentName = "revision-gc-controller"

// NewController creates a new Garbage Collection controller
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	logger := logging.FromContext(ctx)
	configurationInformer := configurationinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)

	c := &reconciler{
		client:         servingclient.Get(ctx),
		revisionLister: revisionInformer.Lister(),
	}
	return configreconciler.NewImpl(ctx, c, func(impl *controller.Impl) controller.Options {
		logger.Info("Setting up event handlers")

		configurationInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(old interface{}, new interface{}) {
				if c, ok := new.(*v1.Configuration); ok && (c.IsReady() || c.IsFailed()) {
					impl.Enqueue(new)
				}
			},
		})

		logger.Info("Setting up ConfigMap receivers with resync func")
		configsToResync := []interface{}{
			&gcconfig.Config{},
		}
		resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
			// Triggers syncs on all revisions when configuration changes.
			impl.GlobalResync(revisionInformer.Informer())
		})

		logger.Info("Setting up ConfigMap receivers")
		configStore := configns.NewStore(logging.WithLogger(ctx, logger.Named("config-store")), resync)
		configStore.WatchConfigs(cmw)

		return controller.Options{
			ConfigStore: configStore,
			AgentName:   controllerAgentName,
			// The GC reconciler shouldn't mutate the revision's status.
			SkipStatusUpdates: true,
		}
	})
}
