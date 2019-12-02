/*
Copyright 2019 The Knative Authors

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

package kpa

import (
	"context"

	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	"knative.dev/serving/pkg/client/injection/ducks/autoscaling/v1alpha1/podscalable"
	metricinformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric"
	painformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler"
	sksinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/serverlessservice"

	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/autoscaler"
	"knative.dev/serving/pkg/reconciler"
	areconciler "knative.dev/serving/pkg/reconciler/autoscaling"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
	"knative.dev/serving/pkg/reconciler/autoscaling/kpa/resources"
)

const controllerAgentName = "kpa-class-podautoscaler-controller"

// NewController returns a new KPA reconcile controller.
// TODO(mattmoor): Fix the signature to adhere to the injection type.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
	deciders resources.Deciders,
) *controller.Impl {

	paInformer := painformer.Get(ctx)
	sksInformer := sksinformer.Get(ctx)
	serviceInformer := serviceinformer.Get(ctx)
	endpointsInformer := endpointsinformer.Get(ctx)
	metricInformer := metricinformer.Get(ctx)
	psInformerFactory := podscalable.Get(ctx)

	c := &Reconciler{
		Base: &areconciler.Base{
			Base:              reconciler.NewBase(ctx, controllerAgentName, cmw),
			PALister:          paInformer.Lister(),
			SKSLister:         sksInformer.Lister(),
			ServiceLister:     serviceInformer.Lister(),
			MetricLister:      metricInformer.Lister(),
			PSInformerFactory: psInformerFactory,
		},
		endpointsLister: endpointsInformer.Lister(),
		deciders:        deciders,
	}
	impl := controller.NewImpl(c, c.Logger, "KPA-Class Autoscaling")
	c.scaler = newScaler(ctx, psInformerFactory, impl.EnqueueAfter)

	c.Logger.Info("Setting up KPA-Class event handlers")
	// Handle only PodAutoscalers that have KPA annotation.
	onlyKpaClass := reconciler.AnnotationFilterFunc(
		autoscaling.ClassAnnotationKey, autoscaling.KPA, false /*allowUnset*/)
	paHandler := cache.FilteringResourceEventHandler{
		FilterFunc: onlyKpaClass,
		Handler:    controller.HandleAll(impl.Enqueue),
	}
	paInformer.Informer().AddEventHandler(paHandler)

	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: reconciler.LabelExistsFilterFunc(autoscaling.KPALabelKey),
		Handler:    controller.HandleAll(impl.EnqueueLabelOfNamespaceScopedResource("", autoscaling.KPALabelKey)),
	})

	// Watch all the services that we have created.
	serviceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: onlyKpaClass,
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})
	sksInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: onlyKpaClass,
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	metricInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: onlyKpaClass,
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	// Have the Deciders enqueue the PAs whose decisions have changed.
	deciders.Watch(impl.EnqueueKey)

	c.Logger.Info("Setting up ConfigMap receivers")
	configsToResync := []interface{}{
		&autoscaler.Config{},
	}
	resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		impl.FilteredGlobalResync(onlyKpaClass, paInformer.Informer())
	})
	configStore := config.NewStore(c.Logger.Named("config-store"), resync)
	configStore.WatchConfigs(cmw)
	c.ConfigStore = configStore

	return impl
}
