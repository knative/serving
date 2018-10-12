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

package v1alpha1

import (
	"time"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging/logkey"
	"github.com/knative/pkg/tracker"
	kubescheme "github.com/knative/serving/pkg/client/clientset/versioned/scheme"
	servingscheme "github.com/knative/serving/pkg/client/clientset/versioned/scheme"
	"github.com/knative/serving/pkg/reconciler"
	"go.uber.org/zap"
	kubeapicorev1 "k8s.io/api/core/v1"
	kubetypedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

func init() {
	// Add serving types to the default Kubernetes Scheme so Events can be
	// logged for serving types.
	servingscheme.AddToScheme(kubescheme.Scheme)
}

type newReconciler func(reconciler.CommonOptions, *DependencyFactory) reconciler.Reconciler

func NewController(
	logger *zap.SugaredLogger,
	newFunc newReconciler,
	componentName string,
	queueName string,
	watcher configmap.Watcher,
	deps *DependencyFactory,
	trackerLease time.Duration,
) (*controller.Impl, error) {

	// There's a circular dependency between
	// the reconciler and the host controller
	//
	// controller -> reconciler -> tracker -> controller
	//
	// We'll set the reconciler after it's been constructed
	c := controller.NewImpl(nil, logger, queueName)

	logger = logger.Named(componentName).
		With(logkey.ControllerType, componentName)

	recorder := newRecorder(componentName, logger, deps)
	tracker := tracker.New(c.EnqueueKey, trackerLease)

	rec := newFunc(reconciler.CommonOptions{
		Logger:           logger,
		Recorder:         recorder,
		ObjectTracker:    tracker,
		WorkQueue:        c,
		ConfigMapWatcher: watcher,
	}, deps)

	if err := reconciler.SetupTriggers(rec, c, tracker, deps); err != nil {
		return nil, err
	}

	if phasedRec, ok := rec.(reconciler.WithPhases); ok {
		for _, phase := range phasedRec.Phases() {
			if err := reconciler.SetupTriggers(phase, c, tracker, deps); err != nil {
				return nil, err
			}
		}
	}

	c.Reconciler = rec

	return c, nil
}

func newRecorder(
	name string,
	logger *zap.SugaredLogger,
	factory *DependencyFactory,
) record.EventRecorder {

	// Create event broadcaster
	logger.Debugf("Creating event broadcaster for %q", name)

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(logger.Named("event-broadcaster").Infof)
	broadcaster.StartRecordingToSink(&kubetypedcorev1.EventSinkImpl{
		Interface: factory.Kubernetes.Client.CoreV1().Events(""),
	})

	return broadcaster.NewRecorder(
		kubescheme.Scheme,
		kubeapicorev1.EventSource{Component: name},
	)
}
