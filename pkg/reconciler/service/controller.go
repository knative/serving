/*
Copyright 2018 The Knative Authors

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

package service

import (
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "service-controller"
)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
func NewController(
	opt reconciler.Options,
	serviceInformer servinginformers.ServiceInformer,
	configurationInformer servinginformers.ConfigurationInformer,
	revisionInformer servinginformers.RevisionInformer,
	routeInformer servinginformers.RouteInformer,
) *controller.Impl {

	c := &Reconciler{
		Base:                reconciler.NewBase(opt, controllerAgentName),
		serviceLister:       serviceInformer.Lister(),
		configurationLister: configurationInformer.Lister(),
		revisionLister:      revisionInformer.Lister(),
		routeLister:         routeInformer.Lister(),
	}
	impl := controller.NewImpl(c, c.Logger, ReconcilerName, reconciler.MustNewStatsReporter(ReconcilerName, c.Logger))

	c.Logger.Info("Setting up event handlers")
	serviceInformer.Informer().AddEventHandler(reconciler.Handler(impl.Enqueue))

	configurationInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Service")),
		Handler:    reconciler.Handler(impl.EnqueueControllerOf),
	})

	routeInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Service")),
		Handler:    reconciler.Handler(impl.EnqueueControllerOf),
	})

	return impl
}
