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

package main

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/webhook"
	"knative.dev/pkg/webhook/certificates"
	"knative.dev/pkg/webhook/resourcesemantics"
	"knative.dev/pkg/webhook/resourcesemantics/defaulting"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	"knative.dev/serving/pkg/reconciler/ingress"
)

func NewDefaultingAdmissionController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	return defaulting.NewAdmissionController(ctx,

		// Name of the resource webhook.
		"webhook.istio.networking.internal.knative.dev",

		// The path on which to serve the webhook.
		"/defaulting",

		// The resources to default.
		map[schema.GroupVersionKind]resourcesemantics.GenericCRD{
			v1alpha1.SchemeGroupVersion.WithKind("Revision"): &RevisionStub{},
			v1beta1.SchemeGroupVersion.WithKind("Revision"):  &RevisionStub{},
			v1.SchemeGroupVersion.WithKind("Revision"):       &RevisionStub{},
		},

		// no need to decorate the context
		nil,

		// allow unknown fields since our stub type only has a subset of them
		false,
	)
}

func main() {
	// Set up a signal context with our webhook options
	ctx := webhook.WithOptions(signals.NewContext(), webhook.Options{
		ServiceName: "istio-webhook",
		Port:        8443,
		SecretName:  "istio-webhook-certs",
	})

	sharedmain.WebhookMainWithContext(ctx, "istiocontroller",
		NewDefaultingAdmissionController,
		certificates.NewController,
		ingress.NewController)
}
