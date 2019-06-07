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

package configuration

import (
	"context"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	configns "github.com/knative/serving/pkg/reconciler/configuration/config"
	"k8s.io/client-go/tools/cache"
)

const controllerAgentName = "configuration-controller"

type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
}

// NewController creates a new Configuration controller
func NewController(
	opt reconciler.Options,
	configurationInformer servinginformers.ConfigurationInformer,
	revisionInformer servinginformers.RevisionInformer,
) *controller.Impl {

	c := &Reconciler{
		Base:                reconciler.NewBase(opt, controllerAgentName),
		configurationLister: configurationInformer.Lister(),
		revisionLister:      revisionInformer.Lister(),
	}
	impl := controller.NewImpl(c, c.Logger, "Configurations")

	c.Logger.Info("Setting up event handlers")
	configurationInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	revisionInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Configuration")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	c.Logger.Info("Setting up ConfigMap receivers")
	c.configStore = configns.NewStore(c.Logger.Named("config-store"), opt.ResyncPeriod)
	c.configStore.WatchConfigs(opt.ConfigMapWatcher)
	return impl
}
