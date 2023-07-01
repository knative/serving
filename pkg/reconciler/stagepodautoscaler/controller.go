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

package stagepodautoscaler

import (
	"context"
	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/apis/serving"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	painformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler"
	spainformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/stagepodautoscaler"
	spareconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/stagepodautoscaler"
)

// NewController creates a new stagepodautoscaler controller
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	//logger := logging.FromContext(ctx)
	paInformer := painformer.Get(ctx)
	stagePodAutoscalerInformer := spainformer.Get(ctx)

	//configStore := config.NewStore(logger.Named("config-store"))
	//configStore.WatchConfigs(cmw)

	c := &Reconciler{
		client:              servingclient.Get(ctx),
		podAutoscalerLister: paInformer.Lister(),
	}
	impl := spareconciler.NewImpl(ctx, c)

	stagePodAutoscalerInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	handleMatchingControllers1 := cache.FilteringResourceEventHandler{
		FilterFunc: pkgreconciler.LabelExistsFilterFunc(serving.RevisionLabelKey),
		Handler:    controller.HandleAll(impl.EnqueueLabelOfNamespaceScopedResource("", serving.RevisionLabelKey)),
	}
	paInformer.Informer().AddEventHandler(handleMatchingControllers1)

	return impl

}
