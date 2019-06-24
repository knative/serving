/*
Copyright 2019 The Knative Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package serverlessservice

import (
	"context"

	endpointsinformer "github.com/knative/pkg/injection/informers/kubeinformers/corev1/endpoints"
	serviceinformer "github.com/knative/pkg/injection/informers/kubeinformers/corev1/service"
	sksinformer "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/serverlessservice"
	pkgreconciler "github.com/knative/serving/pkg/reconciler"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/system"
	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/apis/networking"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	rbase "github.com/knative/serving/pkg/reconciler"
	presources "github.com/knative/serving/pkg/resources"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "serverlessservice-controller"
)

// NewController initializes the controller and is called by the generated code.
// Registers eventhandlers to enqueue events.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	serviceInformer := serviceinformer.Get(ctx)
	endpointsInformer := endpointsinformer.Get(ctx)
	sksInformer := sksinformer.Get(ctx)

	c := &reconciler{
		Base:              rbase.NewBase(ctx, controllerAgentName, cmw),
		endpointsLister:   endpointsInformer.Lister(),
		serviceLister:     serviceInformer.Lister(),
		sksLister:         sksInformer.Lister(),
		psInformerFactory: presources.NewPodScalableInformerFactory(ctx),
	}
	impl := controller.NewImpl(c, c.Logger, reconcilerName)

	c.Logger.Info("Setting up event handlers")

	// Watch all the SKS objects.
	sksInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	// Watch all the endpoints that we have attached our label to.
	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: pkgreconciler.LabelExistsFilterFunc(networking.SKSLabelKey),
		Handler:    controller.HandleAll(impl.EnqueueLabelOfNamespaceScopedResource("" /*any namespace*/, networking.SKSLabelKey)),
	})

	// Watch all the services that we have created.
	serviceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(netv1alpha1.SchemeGroupVersion.WithKind("ServerlessService")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	// Watch activator-service endpoints.
	grCb := func(obj interface{}) {
		// Since changes in the Activator Service endpoints affect all the SKS objects,
		// do a global resync.
		c.Logger.Info("Doing a global resync due to activator endpoint changes")
		impl.GlobalResync(sksInformer.Informer())
	}
	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		// Accept only ActivatorService K8s service objects.
		FilterFunc: rbase.ChainFilterFuncs(
			rbase.NamespaceFilterFunc(system.Namespace()),
			rbase.NameFilterFunc(activator.K8sServiceName)),
		Handler: controller.HandleAll(grCb),
	})

	return impl
}
