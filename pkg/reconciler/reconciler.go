/*
Copyright 2018 The Knative Authors

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

package reconciler

import (
	"context"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	cachingclient "knative.dev/caching/pkg/client/injection/client"
	sharedclient "knative.dev/pkg/client/injection/client"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/injection/clients/dynamicclient"
	servingclient "knative.dev/serving/pkg/client/injection/client"

	cachingclientset "knative.dev/caching/pkg/client/clientset/versioned"
	sharedclientset "knative.dev/pkg/client/clientset/versioned"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	servingScheme "knative.dev/serving/pkg/client/clientset/versioned/scheme"
)

const (
	ForceUpgradePatch = `[{
  "op":"add",
  "path":"/metadata/annotations/serving.knative.dev~1forceUpgrade",
  "value":"true"
}]`
)

// ConfigStore is a minimal interface to the config stores used by our controllers.
type ConfigStore interface {
	ToContext(ctx context.Context) context.Context
}

// Base implements the core controller logic, given a Reconciler.
type Base struct {
	// KubeClientSet allows us to talk to the k8s for core APIs
	KubeClientSet kubernetes.Interface

	// SharedClientSet allows us to configure shared objects
	SharedClientSet sharedclientset.Interface

	// ServingClientSet allows us to configure Serving objects
	ServingClientSet clientset.Interface

	// DynamicClientSet allows us to configure pluggable Build objects
	DynamicClientSet dynamic.Interface

	// CachingClientSet allows us to instantiate Image objects
	CachingClientSet cachingclientset.Interface

	// ConfigMapWatcher allows us to watch for ConfigMap changes.
	ConfigMapWatcher configmap.Watcher

	// Recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	Recorder record.EventRecorder

	// StatsReporter reports reconciler's metrics.
	StatsReporter StatsReporter

	// Sugared logger is easier to use but is not as performant as the
	// raw logger. In performance critical paths, call logger.Desugar()
	// and use the returned raw logger instead. In addition to the
	// performance benefits, raw logger also preserves type-safety at
	// the expense of slightly greater verbosity.
	Logger *zap.SugaredLogger
}

// NewBase instantiates a new instance of Base implementing
// the common & boilerplate code between our reconcilers.
func NewBase(ctx context.Context, controllerAgentName string, cmw configmap.Watcher) *Base {
	// Enrich the logs with controller name
	logger := logging.FromContext(ctx).
		Named(controllerAgentName).
		With(zap.String(logkey.ControllerType, controllerAgentName))

	kubeClient := kubeclient.Get(ctx)

	recorder := controller.GetEventRecorder(ctx)
	if recorder == nil {
		// Create event broadcaster
		logger.Debug("Creating event broadcaster")
		eventBroadcaster := record.NewBroadcaster()
		watches := []watch.Interface{
			eventBroadcaster.StartLogging(logger.Named("event-broadcaster").Infof),
			eventBroadcaster.StartRecordingToSink(
				&typedcorev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")}),
		}
		recorder = eventBroadcaster.NewRecorder(
			scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
		go func() {
			<-ctx.Done()
			for _, w := range watches {
				w.Stop()
			}
		}()
	}

	statsReporter := GetStatsReporter(ctx)
	if statsReporter == nil {
		logger.Debug("Creating stats reporter")
		var err error
		statsReporter, err = NewStatsReporter(controllerAgentName)
		if err != nil {
			logger.Fatal(err)
		}
	}

	base := &Base{
		KubeClientSet:    kubeClient,
		SharedClientSet:  sharedclient.Get(ctx),
		DynamicClientSet: dynamicclient.Get(ctx),
		ServingClientSet: servingclient.Get(ctx),
		CachingClientSet: cachingclient.Get(ctx),
		ConfigMapWatcher: cmw,
		Recorder:         recorder,
		StatsReporter:    statsReporter,
		Logger:           logger,
	}

	return base
}

func (b *Base) MarkNeedsUpgrade(gvr schema.GroupVersionResource, namespace, name string) error {
	// Add the annotation serving.knative.dev/forceUpgrade=true to trigger webhook-based defaulting.
	_, err := b.DynamicClientSet.Resource(gvr).Namespace(namespace).Patch(name, types.JSONPatchType,
		[]byte(ForceUpgradePatch), metav1.PatchOptions{})
	return err
}

func init() {
	// Add serving types to the default Kubernetes Scheme so Events can be
	// logged for serving types.
	servingScheme.AddToScheme(scheme.Scheme)
}
