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

package metric

import (
	"context"

	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/autoscaler/metrics"
	metricinformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric"
	painformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler"
	metricreconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/metric"
)

// NewController initializes the controller and is called by the generated code.
// Registers eventhandlers to enqueue events.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
	collector metrics.Collector,
) *controller.Impl {
	metricInformer := metricinformer.Get(ctx)
	paInformer := painformer.Get(ctx)
	c := &reconciler{
		collector: collector,
		paLister:  paInformer.Lister(),
	}
	impl := metricreconciler.NewImpl(ctx, c)

	// Watch all the Metric objects.
	metricInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	// watch pod autoscaler for updates incase of pausing
	onlyKPAClass := pkgreconciler.AnnotationFilterFunc(
		autoscaling.ClassAnnotationKey, autoscaling.KPA, false /*allowUnset*/)
	paInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: onlyKPAClass,
		Handler:    controller.HandleAll(impl.Enqueue),
	})

	collector.Watch(impl.EnqueueKey)

	return impl
}
