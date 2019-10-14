/*
Copyright 2019 The Knative Authors

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

package ingress

import (
	"context"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/tracker"
)

const (
	ingressesWorkQueueName = "Ingresses"
)

// ReconcilerInitializer creates an Ingress Reconciler and exposes methods to perform
// initializations.
type ReconcilerInitializer interface {
	controller.Reconciler

	// Init initializes the reconciler.
	Init(ctx context.Context, cmw configmap.Watcher, impl *controller.Impl)

	// Sets tracker.
	SetTracker(tracker.Interface)
}

// InitializerConstructor constructor function of ReconcilerInitializer
type InitializerConstructor func(ctx context.Context, cmw configmap.Watcher) ReconcilerInitializer

// NewController works as a constructor for Ingress Controller
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	return CreateController(ctx, cmw, ingressesWorkQueueName, newInitializer)
}

// CreateController creates an Ingress Controller
func CreateController(ctx context.Context, cmw configmap.Watcher, workQueueName string,
	initializerCtor InitializerConstructor) *controller.Impl {

	initializer := initializerCtor(ctx, cmw)
	impl := controller.NewImpl(initializer, logging.FromContext(ctx), workQueueName)
	initializer.Init(ctx, cmw, impl)
	return impl
}
