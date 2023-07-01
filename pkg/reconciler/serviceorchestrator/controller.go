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

package serviceorchestrator

import (
	"context"
	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	soinformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/serviceorchestrator"
	spainformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/stagepodautoscaler"
	soreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/serviceorchestrator"
)

// NewController creates a new serviceorchestrator controller
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	soInformer := soinformer.Get(ctx)
	stagePodAutoscalerInformer := spainformer.Get(ctx)

	//configStore := config.NewStore(logger.Named("config-store"))
	//configStore.WatchConfigs(cmw)

	c := &Reconciler{
		client:                   servingclient.Get(ctx),
		stagePodAutoscalerLister: stagePodAutoscalerInformer.Lister(),
	}
	impl := soreconciler.NewImpl(ctx, c)

	soInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	handleMatchingControllers := cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterController(&v1.ServiceOrchestrator{}),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	}
	stagePodAutoscalerInformer.Informer().AddEventHandler(handleMatchingControllers)
	//handleMatchingControllers1 := cache.FilteringResourceEventHandler{
	//	FilterFunc: pkgreconciler.LabelExistsFilterFunc(serving.RevisionLabelKey),
	//	Handler:    controller.HandleAll(impl.EnqueueLabelOfNamespaceScopedResource("", serving.RevisionLabelKey)),
	//}
	//paInformer.Informer().AddEventHandler(handleMatchingControllers1)

	//handleMatchingControllers1 := cache.FilteringResourceEventHandler{
	//	FilterFunc: controller.FilterController(&v1.Revision{}),
	//	Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	//}
	//paInformer.Informer().AddEventHandler(handleMatchingControllers1)
	//paInformer.Informer().AddEventHandler(controller.HandleAll(
	//	controller.EnsureTypeMeta(
	//		impl.Tracker.OnChanged,
	//		v1alpha1.SchemeGroupVersion.WithKind("PodAutoscaler"),
	//	),
	//))

	return impl

}
