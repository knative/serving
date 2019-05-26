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

	cachinginformers "github.com/knative/caching/pkg/client/informers/externalversions/caching/v1alpha1"
	"github.com/knative/pkg/apis/duck"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/tracker"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	painformers "github.com/knative/serving/pkg/client/informers/externalversions/autoscaling/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	"github.com/knative/serving/pkg/deployment"
	"github.com/knative/serving/pkg/metrics"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/revision/config"
	appsv1informers "k8s.io/client-go/informers/apps/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "revision-controller"
)

type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
	Load() *config.Config
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(
	opt reconciler.Options,
	revisionInformer servinginformers.RevisionInformer,
	podAutoscalerInformer painformers.PodAutoscalerInformer,
	imageInformer cachinginformers.ImageInformer,
	deploymentInformer appsv1informers.DeploymentInformer,
	serviceInformer corev1informers.ServiceInformer,
	endpointsInformer corev1informers.EndpointsInformer,
	configMapInformer corev1informers.ConfigMapInformer,
	buildInformerFactory duck.InformerFactory,
) *controller.Impl {
	transport := http.DefaultTransport
	if rt, err := newResolverTransport(k8sCertPath); err != nil {
		opt.Logger.Errorf("Failed to create resolver transport: %v", err)
	} else {
		transport = rt
	}

	c := &Reconciler{
		Base:                reconciler.NewBase(opt, controllerAgentName),
		revisionLister:      revisionInformer.Lister(),
		podAutoscalerLister: podAutoscalerInformer.Lister(),
		imageLister:         imageInformer.Lister(),
		deploymentLister:    deploymentInformer.Lister(),
		serviceLister:       serviceInformer.Lister(),
		endpointsLister:     endpointsInformer.Lister(),
		configMapLister:     configMapInformer.Lister(),
		resolver: &digestResolver{
			client:    opt.KubeClientSet,
			transport: transport,
		},
	}
	impl := controller.NewImpl(c, c.Logger, "Revisions", reconciler.MustNewStatsReporter("Revisions", c.Logger))

	// Set up an event handler for when the resource types of interest change
	c.Logger.Info("Setting up event handlers")
	revisionInformer.Informer().AddEventHandler(reconciler.Handler(impl.Enqueue))

	endpointsInformer.Informer().AddEventHandler(reconciler.Handler(
		impl.EnqueueLabelOfNamespaceScopedResource("", serving.RevisionLabelKey)))

	deploymentInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Revision")),
		Handler:    reconciler.Handler(impl.EnqueueControllerOf),
	})

	podAutoscalerInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Revision")),
		Handler:    reconciler.Handler(impl.EnqueueControllerOf),
	})

	c.tracker = tracker.New(impl.EnqueueKey, opt.GetTrackerLease())

	// We don't watch for changes to Image because we don't incorporate any of its
	// properties into our own status and should work completely in the absence of
	// a functioning Image controller.

	configMapInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Revision")),
		Handler:    reconciler.Handler(impl.EnqueueControllerOf),
	})

	c.buildInformerFactory = newDuckInformerFactory(c.tracker, buildInformerFactory)

	configsToResync := []interface{}{
		&network.Config{},
		&metrics.ObservabilityConfig{},
		&deployment.Config{},
	}

	resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		// Triggers syncs on all revisions when configuration
		// changes
		impl.GlobalResync(revisionInformer.Informer())
	})

	c.configStore = config.NewStore(c.Logger.Named("config-store"), resync)
	c.configStore.WatchConfigs(opt.ConfigMapWatcher)

	return impl
}

func KResourceTypedInformerFactory(opt reconciler.Options) duck.InformerFactory {
	return &duck.TypedInformerFactory{
		Client:       opt.DynamicClientSet,
		Type:         &duckv1alpha1.KResource{},
		ResyncPeriod: opt.ResyncPeriod,
		StopChannel:  opt.StopChannel,
	}
}

func newDuckInformerFactory(t tracker.Interface, delegate duck.InformerFactory) duck.InformerFactory {
	return &duck.CachedInformerFactory{
		Delegate: &duck.EnqueueInformerFactory{
			Delegate:     delegate,
			EventHandler: reconciler.Handler(t.OnChanged),
		},
	}
}
