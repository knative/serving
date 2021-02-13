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

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/client-go/tools/cache"

	v1 "knative.dev/serving/pkg/apis/serving/v1"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	configurationinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	routeinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/route"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"
	"knative.dev/serving/pkg/reconciler/configuration/config"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
)

// NewController wraps a new instance of the labeler that labels
// Configurations with Routes in a controller.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	logger := logging.FromContext(ctx)
	routeInformer := routeinformer.Get(ctx)
	configInformer := configurationinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)

	logger.Info("Setting up ConfigMap receivers")
	configStore := config.NewStore(logger.Named("config-store"))
	configStore.WatchConfigs(cmw)

	c := &Reconciler{}
	impl := routereconciler.NewImpl(ctx, c, func(*controller.Impl) controller.Options {
		return controller.Options{
			ConfigStore: configStore,
			// The labeler shouldn't mutate the route's status.
			SkipStatusUpdates: true,
		}
	})

	logger.Info("Setting up event handlers")
	routeInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: impl.Enqueue,
			UpdateFunc: func(old interface{}, new interface{}) {
				r1, r2 := old.(*v1.Route), new.(*v1.Route)
				if r1 == nil || r2 == nil {
					return
				}
				// Enqueue on route spec or status traffic changed.
				if !equality.Semantic.DeepEqual(r1.Spec.Traffic, r2.Spec.Traffic) ||
					!equality.Semantic.DeepEqual(r1.Status.Traffic, r2.Status.Traffic) {
					impl.Enqueue(new)
				}
			},
			DeleteFunc: impl.Enqueue,
		})

	client := servingclient.Get(ctx)
	clock := &clock.RealClock{}
	c.caccV2 = newConfigurationAccessor(client, configInformer.Lister(), configInformer.Informer().GetIndexer(), clock)
	c.raccV2 = newRevisionAccessor(client, revisionInformer.Lister(), revisionInformer.Informer().GetIndexer(), clock)

	return impl
}
