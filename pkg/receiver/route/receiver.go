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

package route

import (
	"github.com/josephburnett/k8sflag/pkg/k8sflag"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/ingress"
	"github.com/knative/serving/pkg/controller/route"
	"github.com/knative/serving/pkg/receiver"
	"go.uber.org/zap"
)

const (
	controllerAgentName = "route-controller"
)

// RevisionRoute represents a single target to route to.
// Basically represents a k8s service representing a specific Revision
// and how much of the traffic goes to it.
type RevisionRoute struct {
	// Name for external routing. Optional
	Name string
	// RevisionName is the underlying revision that we're currently
	// routing to. Could be resolved from the Configuration or
	// specified explicitly in TrafficTarget
	RevisionName string
	// Service is the name of the k8s service we route to.
	// Note we should not put service namespace as a suffix in this field.
	Service string
	// The k8s service namespace
	Namespace string
	Weight    int
}

// Receiver implements the receiver for Route resources.
type Receiver struct {
	*receiver.Base

	// lister indexes properties about Route
	lister listers.RouteLister
	synced cache.InformerSynced

	// don't start the workers until configuration cache have been synced
	configSynced cache.InformerSynced

	// suffix used to construct Route domain.
	controllerConfig controller.Config

	// Autoscale enable scale to zero experiment flag.
	enableScaleToZero *k8sflag.BoolFlag
}

// Receiver implements ingress.Receiver
var _ ingress.Receiver = (*Receiver)(nil)

// New returns an implementation of several receivers suitable for managing the
// lifecycle of a Route.
func New(
	kubeClientSet kubernetes.Interface,
	elaClientSet clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config,
	controllerConfig controller.Config,
	enableScaleToZero *k8sflag.BoolFlag,
	logger *zap.SugaredLogger) route.Receiver {

	// obtain references to a shared index informer for the Routes and
	// Configurations type.
	informer := elaInformerFactory.Serving().V1alpha1().Routes()

	return &Receiver{
		Base: receiver.NewBase(kubeClientSet, elaClientSet, kubeInformerFactory,
			elaInformerFactory, informer.Informer(), controllerAgentName, logger),
		lister:            informer.Lister(),
		synced:            informer.Informer().HasSynced,
		controllerConfig:  controllerConfig,
		enableScaleToZero: enableScaleToZero,
	}
}
