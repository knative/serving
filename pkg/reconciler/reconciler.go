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
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/knative/pkg/injection/clients/dynamicclient"
	"github.com/knative/pkg/injection/clients/kubeclient"
	"github.com/knative/serving/pkg/injection/clients/cachingclient"
	"github.com/knative/serving/pkg/injection/clients/servingclient"
	"github.com/knative/serving/pkg/injection/clients/sharedclient"

	cachingclientset "github.com/knative/caching/pkg/client/clientset/versioned"
	sharedclientset "github.com/knative/pkg/client/clientset/versioned"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/logging/logkey"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	servingScheme "github.com/knative/serving/pkg/client/clientset/versioned/scheme"
)

// This is mutable for testing.
var resetPeriod = 30 * time.Second

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

	recorder := GetEventRecorder(ctx)
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

// erKey is used to associate record.EventRecorders with contexts.
type erKey struct{}

// WithEventRecorder attaches the given record.EventRecorder to the provided context
// in the returned context.
func WithEventRecorder(ctx context.Context, er record.EventRecorder) context.Context {
	return context.WithValue(ctx, erKey{}, er)
}

// GetEventRecorder attempts to look up the record.EventRecorder on a given context.
// It may return null if none is found.
func GetEventRecorder(ctx context.Context) record.EventRecorder {
	untyped := ctx.Value(erKey{})
	if untyped == nil {
		return nil
	}
	return untyped.(record.EventRecorder)
}

func init() {
	// Add serving types to the default Kubernetes Scheme so Events can be
	// logged for serving types.
	servingScheme.AddToScheme(scheme.Scheme)
}
