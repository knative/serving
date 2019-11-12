/*
Copyright 2019 The Knative Authors.

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

package revision

import (
	"context"
	"net/http"

	imageinformer "knative.dev/caching/pkg/client/injection/informers/caching/v1alpha1/image"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	deploymentinformer "knative.dev/pkg/client/injection/kube/informers/apps/v1/deployment"
	configmapinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/configmap"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	painformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision"

	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	apisconfig "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/metrics"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/revision/config"
)

const (
	controllerAgentName = "revision-controller"
)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	transport := http.DefaultTransport
	if rt, err := newResolverTransport(k8sCertPath); err != nil {
		logging.FromContext(ctx).Errorf("Failed to create resolver transport: %v", err)
	} else {
		transport = rt
	}

	deploymentInformer := deploymentinformer.Get(ctx)
	serviceInformer := serviceinformer.Get(ctx)
	configMapInformer := configmapinformer.Get(ctx)
	imageInformer := imageinformer.Get(ctx)
	revisionInformer := revisioninformer.Get(ctx)
	paInformer := painformer.Get(ctx)

	c := &Reconciler{
		Base:                reconciler.NewBase(ctx, controllerAgentName, cmw),
		revisionLister:      revisionInformer.Lister(),
		podAutoscalerLister: paInformer.Lister(),
		imageLister:         imageInformer.Lister(),
		deploymentLister:    deploymentInformer.Lister(),
		serviceLister:       serviceInformer.Lister(),
		configMapLister:     configMapInformer.Lister(),
		resolver: &digestResolver{
			client:    kubeclient.Get(ctx),
			transport: transport,
		},
	}
	impl := controller.NewImpl(c, c.Logger, "Revisions")

	// Set up an event handler for when the resource types of interest change
	c.Logger.Info("Setting up event handlers")
	revisionInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	deploymentInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Revision")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	paInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Revision")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	// We don't watch for changes to Image because we don't incorporate any of its
	// properties into our own status and should work completely in the absence of
	// a functioning Image controller.

	configMapInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Revision")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	configsToResync := []interface{}{
		&network.Config{},
		&metrics.ObservabilityConfig{},
		&deployment.Config{},
		&apisconfig.Defaults{},
	}

	resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		// Triggers syncs on all revisions when configuration
		// changes
		impl.GlobalResync(revisionInformer.Informer())
	})

	configStore := config.NewStore(c.Logger.Named("config-store"), resync)
	configStore.WatchConfigs(c.ConfigMapWatcher)
	c.configStore = configStore

	return impl
}
