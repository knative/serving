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

package psbinding

import (
	"context"

	// Injection stuff
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	mwhinformer "knative.dev/pkg/client/injection/kube/informers/admissionregistration/v1beta1/mutatingwebhookconfiguration"
	secretinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/secret"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
	"knative.dev/pkg/webhook"
)

// Bindable is implemented by Binding resources whose subjects are PodSpecable
// and that want to leverage this shared logic to simplify binding authorship.
type Bindable interface {
	duck.Bindable

	// Do performs this binding's mutation with the specified context on the
	// provided PodSpecable.  The provided context may be decorated by
	// passing a BindableContext to both NewAdmissionController and
	// BaseReconciler.
	Do(context.Context, *duckv1.WithPod)

	// Undo is the dual of Do, it undoes the binding.
	Undo(context.Context, *duckv1.WithPod)
}

// Mutation is the type of the Do/Undo methods.
type Mutation func(context.Context, *duckv1.WithPod)

// ListAll is the type of methods for enumerating all of the Bindables on the
// cluster in order to index the covered types to program the admission webhook.
type ListAll func() ([]Bindable, error)

// GetListAll is a factory method for the ListAll method, which may also be
// supplied with a ResourceEventHandler to register a callback with the Informer
// that sits behind the returned ListAll so that the handler can queue work
// whenever the result of ListAll changes.
type GetListAll func(context.Context, cache.ResourceEventHandler) ListAll

// BindableContext is the type of context decorator methods that may be supplied
// to NewAdmissionController and BaseReconciler.
type BindableContext func(context.Context, Bindable) (context.Context, error)

// NewAdmissionController constructs the webhook portion of the pair of
// reconcilers that implement the semantics of our Binding.
func NewAdmissionController(
	ctx context.Context,
	name, path string,
	gla GetListAll,
	withContext BindableContext,
	reconcilerOptions ...ReconcilerOption,
) *controller.Impl {

	// Extract the assorted things from our context.
	client := kubeclient.Get(ctx)
	mwhInformer := mwhinformer.Get(ctx)
	secretInformer := secretinformer.Get(ctx)
	options := webhook.GetOptions(ctx)

	// Construct the reconciler for the mutating webhook configuration.
	wh := NewReconciler(name, path, options.SecretName, client, mwhInformer.Lister(), secretInformer.Lister(), withContext, reconcilerOptions...)
	c := controller.NewImpl(wh, logging.FromContext(ctx), name)

	// It doesn't matter what we enqueue because we will always Reconcile
	// the named MWH resource.
	handler := controller.HandleAll(c.EnqueueSentinel(types.NamespacedName{}))

	// Reconcile when the named MutatingWebhookConfiguration changes.
	mwhInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterWithName(name),
		Handler:    handler,
	})

	// Reconcile when the cert bundle changes.
	secretInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterWithNameAndNamespace(system.Namespace(), wh.SecretName),
		Handler:    handler,
	})

	// Give the reconciler a way to list all of the Bindable resources,
	// and configure the controller to handle changes to those resources.
	wh.ListAll = gla(ctx, handler)

	return c
}
