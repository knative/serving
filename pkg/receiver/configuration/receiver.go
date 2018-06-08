/*
Copyright 2018 Google LLC.

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
	buildclientset "github.com/knative/build/pkg/client/clientset/versioned"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/configuration"
	"github.com/knative/serving/pkg/controller/revision"
	"go.uber.org/zap"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const controllerAgentName = "configuration-controller"

// Receiver implements the controller for Configuration resources
type Receiver struct {
	*controller.Base

	buildClientSet buildclientset.Interface

	// lister indexes properties about Configuration
	lister listers.ConfigurationLister
	synced cache.InformerSynced
}

// Receiver implements revision.Receiver
var _ revision.Receiver = (*Receiver)(nil)

// Receiver implements configuration.Receiver
var _ configuration.Receiver = (*Receiver)(nil)

// New returns an implementation of several receivers suitable for managing the
// lifecycle of a Configuration.
func New(
	kubeClientSet kubernetes.Interface,
	elaClientSet clientset.Interface,
	buildClientSet buildclientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config,
	controllerConfig controller.Config,
	logger *zap.SugaredLogger) configuration.Receiver {

	// obtain references to a shared index informer for the Configuration
	// and Revision type.
	informer := elaInformerFactory.Serving().V1alpha1().Configurations()

	return &Receiver{
		Base: controller.NewBase(kubeClientSet, elaClientSet, kubeInformerFactory,
			elaInformerFactory, informer.Informer(), controllerAgentName, "Configurations", logger),
		buildClientSet: buildClientSet,
		lister:         informer.Lister(),
		synced:         informer.Informer().HasSynced,
	}
}
