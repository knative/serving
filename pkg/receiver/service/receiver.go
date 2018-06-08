/*
Copyright 2018 Google LLC

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
	"go.uber.org/zap"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/service"
	"github.com/knative/serving/pkg/receiver"
)

// Receiver implements the receiver for Service resources.
type Receiver struct {
	*receiver.Base

	// lister indexes properties about Services
	lister listers.ServiceLister
	synced cache.InformerSynced
}

// Receiver implements service.Receiver
var _ service.Receiver = (*Receiver)(nil)

const (
	controllerAgentName = "service-controller"
)

// New returns an implementation of several receivers suitable for managing the
// lifecycle of a Service.
func New(
	kubeClientSet kubernetes.Interface,
	elaClientSet clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config,
	controllerConfig controller.Config,
	logger *zap.SugaredLogger) service.Receiver {

	// obtain references to a shared index informer for the Services.
	informer := elaInformerFactory.Serving().V1alpha1().Services()

	return &Receiver{
		Base: receiver.NewBase(kubeClientSet, elaClientSet, kubeInformerFactory,
			elaInformerFactory, informer.Informer(), controllerAgentName, logger),
		lister: informer.Lister(),
		synced: informer.Informer().HasSynced,
	}
}
