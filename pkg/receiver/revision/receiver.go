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

package revision

import (
	"net/http"

	"github.com/josephburnett/k8sflag/pkg/k8sflag"
	"go.uber.org/zap"

	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	appsv1 "k8s.io/api/apps/v1"
	kubeinformers "k8s.io/client-go/informers"

	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller/build"
	"github.com/knative/serving/pkg/controller/deployment"
	"github.com/knative/serving/pkg/controller/endpoint"
	"github.com/knative/serving/pkg/controller/revision"
	"github.com/knative/serving/pkg/receiver"
)

type resolver interface {
	Resolve(*appsv1.Deployment) error
}

// Receiver implements the receiver for Revision resources.
type Receiver struct {
	*receiver.Base

	// lister indexes properties about Revision
	lister listers.RevisionLister
	synced cache.InformerSynced

	buildtracker *buildTracker

	resolver resolver

	// enableVarLogCollection dedicates whether to set up a fluentd sidecar to
	// collect logs under /var/log/.
	enableVarLogCollection bool

	// controllerConfig includes the configurations for the controller
	controllerConfig *ControllerConfig
}

// Controller implements service.Receiver
var _ revision.Receiver = (*Receiver)(nil)

// Controller implements build.Receiver
var _ build.Receiver = (*Receiver)(nil)

// Controller implements endpoint.Receiver
var _ endpoint.Receiver = (*Receiver)(nil)

// Controller implements deployment.Receiver
var _ deployment.Receiver = (*Receiver)(nil)

// ControllerConfig includes the configurations for the controller.
type ControllerConfig struct {
	// Autoscale part

	// see (config-autoscaler.yaml)
	AutoscaleConcurrencyQuantumOfTime *k8sflag.DurationFlag
	AutoscaleEnableSingleConcurrency  *k8sflag.BoolFlag

	// AutoscalerImage is the name of the image used for the autoscaler pod.
	AutoscalerImage string

	// QueueSidecarImage is the name of the image used for the queue sidecar
	// injected into the revision pod
	QueueSidecarImage string

	// logging part

	// EnableVarLogCollection dedicates whether to set up a fluentd sidecar to
	// collect logs under /var/log/.
	EnableVarLogCollection bool

	// TODO(#818): Use the fluentd deamon set to collect /var/log.
	// FluentdSidecarImage is the name of the image used for the fluentd sidecar
	// injected into the revision pod. It is used only when enableVarLogCollection
	// is true.
	FluentdSidecarImage string
	// FluentdSidecarOutputConfig is the config for fluentd sidecar to specify
	// logging output destination.
	FluentdSidecarOutputConfig string

	// LoggingURLTemplate is a string containing the logging url template where
	// the variable REVISION_UID will be replaced with the created revision's UID.
	LoggingURLTemplate string

	// QueueProxyLoggingConfig is a string containing the logger configuration for queue proxy.
	QueueProxyLoggingConfig string

	// QueueProxyLoggingLevel is a string containing the logger level for queue proxy.
	QueueProxyLoggingLevel string
}

// New returns an implementation of several receivers suitable for managing the
// lifecycle of a Revision.
func New(
	kubeClientSet kubernetes.Interface,
	elaClientSet clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config,
	controllerConfig *ControllerConfig,
	logger *zap.SugaredLogger) revision.Receiver {

	// obtain references to a shared index informer for the Revision type.
	informer := elaInformerFactory.Serving().V1alpha1().Revisions()

	return &Receiver{
		Base: receiver.NewBase(kubeClientSet, elaClientSet, kubeInformerFactory,
			elaInformerFactory, informer.Informer(), controllerAgentName, logger),
		lister:           informer.Lister(),
		synced:           informer.Informer().HasSynced,
		buildtracker:     &buildTracker{builds: map[key]set{}},
		resolver:         &digestResolver{client: kubeClientSet, transport: http.DefaultTransport},
		controllerConfig: controllerConfig,
	}
}
