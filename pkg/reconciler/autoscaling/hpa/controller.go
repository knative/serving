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
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/autoscaling"
	informers "github.com/knative/serving/pkg/client/informers/externalversions/autoscaling/v1alpha1"
	ninformers "github.com/knative/serving/pkg/client/informers/externalversions/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	autoscalingv1informers "k8s.io/client-go/informers/autoscaling/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "hpa-class-podautoscaler-controller"
)

// NewController returns a new HPA reconcile controller.
func NewController(
	opts *reconciler.Options,
	paInformer informers.PodAutoscalerInformer,
	sksInformer ninformers.ServerlessServiceInformer,
	hpaInformer autoscalingv1informers.HorizontalPodAutoscalerInformer,
) *controller.Impl {
	c := &Reconciler{
		Base:      reconciler.NewBase(*opts, controllerAgentName),
		paLister:  paInformer.Lister(),
		hpaLister: hpaInformer.Lister(),
		sksLister: sksInformer.Lister(),
	}
	impl := controller.NewImpl(c, c.Logger, "HPA-Class Autoscaling", reconciler.MustNewStatsReporter("HPA-Class Autoscaling", c.Logger))

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
