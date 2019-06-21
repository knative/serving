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

	"github.com/knative/pkg/apis/duck"
	endpointsinformer "github.com/knative/pkg/injection/informers/kubeinformers/corev1/endpoints"
	serviceinformer "github.com/knative/pkg/injection/informers/kubeinformers/corev1/service"
	kpainformer "github.com/knative/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler"
	sksinformer "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/serverlessservice"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/autoscaling"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/reconciler"
	areconciler "github.com/knative/serving/pkg/reconciler/autoscaling"
	"github.com/knative/serving/pkg/reconciler/autoscaling/config"
	"github.com/knative/serving/pkg/reconciler/autoscaling/kpa/resources"
	aresources "github.com/knative/serving/pkg/reconciler/autoscaling/resources"
	"k8s.io/client-go/tools/cache"
)

const controllerAgentName = "kpa-class-podautoscaler-controller"

// NewController returns a new HPA reconcile controller.
// TODO(mattmoor): Fix the signature to adhere to the injection type.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
	deciders resources.Deciders,
	metrics aresources.Metrics,
	psInformerFactory duck.InformerFactory,
) *controller.Impl {

	paInformer := kpainformer.Get(ctx)
	sksInformer := sksinformer.Get(ctx)
	serviceInformer := serviceinformer.Get(ctx)
	endpointsInformer := endpointsinformer.Get(ctx)

	c := &Reconciler{
		Base: &areconciler.Base{
			Base:              reconciler.NewBase(ctx, controllerAgentName, cmw),
			PALister:          paInformer.Lister(),
			SKSLister:         sksInformer.Lister(),
			ServiceLister:     serviceInformer.Lister(),
			Metrics:           metrics,
			PSInformerFactory: psInformerFactory,
		},
		endpointsLister: endpointsInformer.Lister(),
		deciders:        deciders,
	}
	impl := controller.NewImpl(c, c.Logger, "KPA-Class Autoscaling")
	c.scaler = newScaler(ctx, psInformerFactory, impl.EnqueueAfter)

	c.Logger.Info("Setting up KPA-Class event handlers")
	// Handle PodAutoscalers missing the class annotation for backward compatibility.
	onlyKpaClass := reconciler.AnnotationFilterFunc(autoscaling.ClassAnnotationKey, autoscaling.KPA, true)
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

	// Have the Deciders enqueue the PAs whose decisions have changed.
	deciders.Watch(impl.EnqueueKey)

	c.Logger.Info("Setting up ConfigMap receivers")
	configsToResync := []interface{}{
		&autoscaler.Config{},
	}
	resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		controller.SendGlobalUpdates(paInformer.Informer(), paHandler)
	})
	configStore := config.NewStore(c.Logger.Named("config-store"), resync)
	configStore.WatchConfigs(cmw)
	c.ConfigStore = configStore

	return impl
}
