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

package hpa

import (
	"context"

	"github.com/knative/pkg/apis/duck"
	hpainformer "github.com/knative/pkg/injection/informers/kubeinformers/autoscalingv2beta1/hpa"
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
	aresources "github.com/knative/serving/pkg/reconciler/autoscaling/resources"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "hpa-class-podautoscaler-controller"
)

// NewController returns a new HPA reconcile controller.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
	metrics aresources.Metrics,
	psInformerFactory duck.InformerFactory,
) *controller.Impl {

	paInformer := kpainformer.Get(ctx)
	sksInformer := sksinformer.Get(ctx)
	hpaInformer := hpainformer.Get(ctx)
	serviceInformer := serviceinformer.Get(ctx)

	c := &Reconciler{
		Base: &areconciler.Base{
			Base:              reconciler.NewBase(ctx, controllerAgentName, cmw),
			PALister:          paInformer.Lister(),
			SKSLister:         sksInformer.Lister(),
			ServiceLister:     serviceInformer.Lister(),
			Metrics:           metrics,
			PSInformerFactory: psInformerFactory,
		},
		hpaLister: hpaInformer.Lister(),
	}
	impl := controller.NewImpl(c, c.Logger, "HPA-Class Autoscaling")

	c.Logger.Info("Setting up hpa-class event handlers")
	onlyHpaClass := reconciler.AnnotationFilterFunc(autoscaling.ClassAnnotationKey, autoscaling.HPA, false)
	paHandler := cache.FilteringResourceEventHandler{
		FilterFunc: onlyHpaClass,
		Handler:    controller.HandleAll(impl.Enqueue),
	}
	paInformer.Informer().AddEventHandler(paHandler)

	hpaInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: onlyHpaClass,
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	sksInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: onlyHpaClass,
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

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
