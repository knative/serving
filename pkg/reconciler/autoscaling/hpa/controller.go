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

	networkingclient "knative.dev/networking/pkg/client/injection/client"
	sksinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/serverlessservice"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	hpainformer "knative.dev/pkg/client/injection/kube/informers/autoscaling/v2beta1/horizontalpodautoscaler"
	"knative.dev/pkg/logging"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	metricinformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric"
	painformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler"
	pareconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/podautoscaler"
	"knative.dev/serving/pkg/deployment"

	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/apis/autoscaling"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/config/sharedconfig"
	servingreconciler "knative.dev/serving/pkg/reconciler"
	areconciler "knative.dev/serving/pkg/reconciler/autoscaling"
	"knative.dev/serving/pkg/reconciler/autoscaling/config"
)

const controllerAgentName = "hpa-class-podautoscaler-controller"

// NewController returns a new HPA reconcile controller.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	ctx = servingreconciler.AnnotateLoggerWithName(ctx, controllerAgentName)
	logger := logging.FromContext(ctx)
	paInformer := painformer.Get(ctx)
	sksInformer := sksinformer.Get(ctx)
	hpaInformer := hpainformer.Get(ctx)
	metricInformer := metricinformer.Get(ctx)

	onlyHPAClass := pkgreconciler.AnnotationFilterFunc(autoscaling.ClassAnnotationKey, autoscaling.HPA, false)

	c := &Reconciler{
		Base: &areconciler.Base{
			Client:           servingclient.Get(ctx),
			NetworkingClient: networkingclient.Get(ctx),
			SKSLister:        sksInformer.Lister(),
			MetricLister:     metricInformer.Lister(),
		},

		kubeClient: kubeclient.Get(ctx),
		hpaLister:  hpaInformer.Lister(),
	}
	impl := pareconciler.NewImpl(ctx, c, autoscaling.HPA, func(impl *controller.Impl) controller.Options {
		logger.Info("Setting up ConfigMap receivers")
		configsToResync := []interface{}{
			&sharedconfig.Config{},
			&deployment.Config{},
		}
		resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
			impl.FilteredGlobalResync(onlyHPAClass, paInformer.Informer())
		})
		configStore := config.NewStore(logger.Named("config-store"), resync)
		configStore.WatchConfigs(cmw)
		return controller.Options{ConfigStore: configStore}
	})

	logger.Info("Setting up hpa-class event handlers")

	paInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: onlyHPAClass,
		Handler:    controller.HandleAll(impl.Enqueue),
	})

	onlyPAControlled := controller.FilterControllerGVK(av1alpha1.SchemeGroupVersion.WithKind("PodAutoscaler"))
	handleMatchingControllers := cache.FilteringResourceEventHandler{
		FilterFunc: pkgreconciler.ChainFilterFuncs(onlyHPAClass, onlyPAControlled),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	}
	hpaInformer.Informer().AddEventHandler(handleMatchingControllers)
	sksInformer.Informer().AddEventHandler(handleMatchingControllers)
	metricInformer.Informer().AddEventHandler(handleMatchingControllers)

	return impl
}
