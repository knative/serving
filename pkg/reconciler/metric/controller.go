/*
Copyright 2019 The Knative Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metric

import (
	"context"

	"go.uber.org/zap"
	"k8s.io/client-go/tools/cache"
	"knative.dev/serving/pkg/autoscaler/metrics"
	metricinformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric"
	metricreconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/metric"
	pkgreconciler "knative.dev/serving/pkg/reconciler"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
)

const (
	controllerAgentName = "metric-controller"
)

// NewController initializes the controller and is called by the generated code.
// Registers eventhandlers to enqueue events.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
	collector metrics.Collector,
) *controller.Impl {
	metricInformer := metricinformer.Get(ctx)

	c := &reconciler{
		Base:         pkgreconciler.NewBase(ctx, controllerAgentName, cmw),
		collector:    collector,
		metricLister: metricInformer.Lister(),
	}
	impl := metricreconciler.NewImpl(ctx, c)

	c.Logger.Info("Setting up event handlers")

	// Watch all the Metric objects.
	metricInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	metricInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			accessor, err := kmeta.DeletionHandlingAccessor(obj)
			if err != nil {
				c.Logger.Errorw("Error accessing object", zap.Error(err))
				return
			}
			c.collector.Delete(accessor.GetNamespace(), accessor.GetName())
		},
	})

	return impl
}
