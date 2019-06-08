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

	hpainformer "github.com/knative/pkg/injection/informers/kubeinformers/autoscalingv2beta1/hpa"
	kpainformer "github.com/knative/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler"
	sksinformer "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/serverlessservice"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/autoscaling"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/autoscaling/config"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "hpa-class-podautoscaler-controller"
)

// configStore is a minimized interface of the actual configStore.
type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
}

// NewController returns a new HPA reconcile controller.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {

	paInformer := kpainformer.Get(ctx)
	sksInformer := sksinformer.Get(ctx)
	hpaInformer := hpainformer.Get(ctx)

	c := &Reconciler{
		Base:      reconciler.NewBase(ctx, controllerAgentName, cmw),
		paLister:  paInformer.Lister(),
		hpaLister: hpaInformer.Lister(),
		sksLister: sksInformer.Lister(),
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
	c.configStore = config.NewStore(c.Logger.Named("config-store"), resync)
	c.configStore.WatchConfigs(cmw)

	return impl
}
