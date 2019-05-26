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

	"github.com/knative/pkg/injection"
	hpainformer "github.com/knative/pkg/injection/informers/kubeinformers/autoscalingv1/hpa"
	kpainformer "github.com/knative/serving/pkg/injection/informers/servinginformers/kpa"
	sksinformer "github.com/knative/serving/pkg/injection/informers/servinginformers/serverlessservice"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/autoscaling"
	"github.com/knative/serving/pkg/reconciler"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "hpa-class-podautoscaler-controller"
)

func init() {
	injection.Default.RegisterController(NewController)
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
	paInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: onlyHpaClass,
		Handler:    controller.HandleAll(impl.Enqueue),
	})

	hpaInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: onlyHpaClass,
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	sksInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: onlyHpaClass,
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})
	return impl
}
