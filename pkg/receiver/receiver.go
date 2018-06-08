/*
Copyright 2018 Google LLC

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

package receiver

import (
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
)

// Base implements most of the boilerplate and common code
// we have in our receivers.
type Base struct {
	// KubeClientSet allows us to talk to the k8s for core APIs
	KubeClientSet kubernetes.Interface

	// ElaClientSet allows us to configure Ela objects
	ElaClientSet clientset.Interface

	// KubeInformerFactory provides shared informers for resources
	// in all known API group versions
	KubeInformerFactory kubeinformers.SharedInformerFactory

	// ElaInformerFactory provides shared informers for resources
	// in all known API group versions
	ElaInformerFactory informers.SharedInformerFactory

	// Recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	Recorder record.EventRecorder

	// Sugared logger is easier to use but is not as performant as the
	// raw logger. In performance critical paths, call logger.Desugar()
	// and use the returned raw logger instead. In addition to the
	// performance benefits, raw logger also preserves type-safety at
	// the expense of slightly greater verbosity.
	Logger *zap.SugaredLogger
}

// NewBase instantiates a new instance of Base implementing
// the common & boilerplate code between our receivers.
func NewBase(
	kubeClientSet kubernetes.Interface,
	elaClientSet clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	informer cache.SharedIndexInformer,
	receiverAgentName string,
	logger *zap.SugaredLogger) *Base {

	// Enrich the logs with receiver name
	logger = logger.Named(receiverAgentName).With(zap.String(logkey.ReceiverType, receiverAgentName))

	// Create event broadcaster
	logger.Debug("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(logger.Named("event-broadcaster").Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeClientSet.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: receiverAgentName})

	return &Base{
		KubeClientSet:       kubeClientSet,
		ElaClientSet:        elaClientSet,
		KubeInformerFactory: kubeInformerFactory,
		ElaInformerFactory:  elaInformerFactory,
		Recorder:            recorder,
		Logger:              logger,
	}
}
