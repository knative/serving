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

package reconciler

import (
	"context"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/tracker"
	"go.uber.org/zap"
	"k8s.io/client-go/tools/record"
)

type (
	// Phase is a partial step in an object's reconciliation
	//
	// As an example a Knative object's reconciliation may require
	// that N Kubernetes objects are created. A reconciler can be
	// composed with N phases where each phase is responsible for
	// reconciling a single Kubernetes resource. This abstraction
	// is intended to help manage a reconciler's complexity.
	//
	// Phases can specify what should trigger reconcilation by
	// conforming to the 'WithTriggers' interface. eg:
	//
	//	func (p *myPhase) Triggers() []reconciler.Trigger {
	//		return []reconciler.Trigger{{
	//			ObjectKind:  corev1.SchemeGroupVersion.WithKind("Service"),
	//			EnqueueType: reconciler.EnqueueObject,
	//		}}
	//	}
	//
	Phase interface{}

	// Reconciler is the interface that controller implementations are expected
	// to implement, so that the shared controller.Impl can drive work through it.
	Reconciler interface {
		Reconcile(ctx context.Context, key string) error
	}

	// WithPhases is an interface a reconciler may conform to
	// in order to surface it's phases
	//
	// The motivation to expose a reconciler's phases is to setup
	// any phase triggers
	WithPhases interface {
		Phases() []Phase
	}

	// ConfigStore is responsbile for storing it's configuration into context
	// Subsequently it instructs the configmap.Watcher to watch certain configs
	// a reconciler may be interested in.
	ConfigStore interface {
		ToContext(context.Context) context.Context
		WatchConfigs(configmap.Watcher)
	}

	// WorkQueue is a consumer interface which exposes the underlying work queue
	// to the reconciler
	WorkQueue interface {
		EnqueueKey(key string)
		Enqueue(obj interface{})
		EnqueueControllerOf(obj interface{})
	}

	// CommonOptions contains the dependencies that are associated with a specific
	// instance of a reconciler
	CommonOptions struct {
		Logger           *zap.SugaredLogger
		Recorder         record.EventRecorder
		ObjectTracker    tracker.Interface
		WorkQueue        WorkQueue
		ConfigMapWatcher configmap.Watcher
	}
)
