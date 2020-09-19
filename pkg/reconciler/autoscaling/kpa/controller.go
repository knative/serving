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

package kpa

import (
	"context"

	"go.uber.org/zap"
	"k8s.io/client-go/tools/cache"

	networkingclient "knative.dev/networking/pkg/client/injection/client"
	sksinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/serverlessservice"
	podinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	"knative.dev/serving/pkg/client/injection/ducks/autoscaling/v1alpha1/podscalable"
	metricinformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric"
	painformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler"
	pareconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/podautoscaler"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/apis/autoscaling"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/deployment"
	servingreconciler "knative.dev/serving/pkg/reconciler"
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
	ctx = servingreconciler.AnnotateLoggerWithName(ctx, controllerAgentName)
	logger := logging.FromContext(ctx)
	paInformer := painformer.Get(ctx)
	sksInformer := sksinformer.Get(ctx)
	podsInformer := podinformer.Get(ctx)
	metricInformer := metricinformer.Get(ctx)
	psInformerFactory := podscalable.Get(ctx)

	onlyKPAClass := pkgreconciler.AnnotationFilterFunc(
		autoscaling.ClassAnnotationKey, autoscaling.KPA, false /*allowUnset*/)

	c := &Reconciler{
		Base: &areconciler.Base{
			Client:           servingclient.Get(ctx),
			NetworkingClient: networkingclient.Get(ctx),
			SKSLister:        sksInformer.Lister(),
			MetricLister:     metricInformer.Lister(),
		},
		podsLister: podsInformer.Lister(),
		deciders:   deciders,
	}
	impl := pareconciler.NewImpl(ctx, c, autoscaling.KPA, func(impl *controller.Impl) controller.Options {
		logger.Info("Setting up ConfigMap receivers")
		configsToResync := []interface{}{
			&autoscalerconfig.Config{},
			&deployment.Config{},
		}
		resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
			impl.FilteredGlobalResync(onlyKPAClass, paInformer.Informer())
		})
		configStore := config.NewStore(logger.Named("config-store"), resync)
		configStore.WatchConfigs(cmw)
		return controller.Options{ConfigStore: configStore}
	})
	c.scaler = newScaler(ctx, psInformerFactory, impl.EnqueueAfter)

	logger.Info("Setting up KPA-Class event handlers")

	paInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: onlyKPAClass,
		Handler:    controller.HandleAll(impl.Enqueue),
	})

	// When we see PodAutoscalers deleted, clean up the decider.
	paInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			accessor, err := kmeta.DeletionHandlingAccessor(obj)
			if err != nil {
				logger.Errorw("Error accessing object", zap.Error(err))
				return
			}
			deciders.Delete(ctx, accessor.GetNamespace(), accessor.GetName())
		},
	})

	onlyPAControlled := controller.FilterControllerGVK(av1alpha1.SchemeGroupVersion.WithKind("PodAutoscaler"))
	handleMatchingControllers := cache.FilteringResourceEventHandler{
		FilterFunc: pkgreconciler.ChainFilterFuncs(onlyKPAClass, onlyPAControlled),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	}
	sksInformer.Informer().AddEventHandler(handleMatchingControllers)
	metricInformer.Informer().AddEventHandler(handleMatchingControllers)

	// Watch the knative pods.
	podsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: pkgreconciler.LabelExistsFilterFunc(serving.RevisionLabelKey),
		Handler:    controller.HandleAll(impl.EnqueueLabelOfNamespaceScopedResource("", serving.RevisionLabelKey)),
	})

	// Have the Deciders enqueue the PAs whose decisions have changed.
	deciders.Watch(impl.EnqueueKey)

	return impl
}
