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

package main

import (
	// The set of controllers this controller process runs.
	"context"
	"flag"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/webhook"
	"knative.dev/pkg/webhook/certificates"
	"knative.dev/pkg/webhook/resourcesemantics"
	"knative.dev/pkg/webhook/resourcesemantics/defaulting"
	"knative.dev/pkg/webhook/resourcesemantics/validation"
	servingv1alpha1 "knative.dev/serving/pkg/apis/serving/v1alpha1"
	servingv1beta1 "knative.dev/serving/pkg/apis/serving/v1beta1"
	"knative.dev/serving/pkg/reconciler/domainmapping"

	certificate "knative.dev/control-protocol/pkg/certificates/reconciler"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/signals"
	"knative.dev/serving/pkg/reconciler/configuration"
	"knative.dev/serving/pkg/reconciler/gc"
	"knative.dev/serving/pkg/reconciler/labeler"
	"knative.dev/serving/pkg/reconciler/nscert"
	"knative.dev/serving/pkg/reconciler/revision"
	"knative.dev/serving/pkg/reconciler/route"
	"knative.dev/serving/pkg/reconciler/serverlessservice"
	"knative.dev/serving/pkg/reconciler/service"

	filteredFactory "knative.dev/pkg/client/injection/kube/informers/factory/filtered"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/reconciler/domainmapping/config"
)

const (
	secretLabelNamePostfix = "-ctrl"
)

var ctors = []injection.ControllerConstructor{
	configuration.NewController,
	labeler.NewController,
	revision.NewController,
	route.NewController,
	serverlessservice.NewController,
	service.NewController,
	gc.NewController,
	nscert.NewController,
	certificate.NewControllerFactory(networking.ServingCertName),
	domainmapping.NewController,
	certificates.NewController,
	newDefaultingAdmissionController,
	newValidatingAdmissionController,
}

var types = map[schema.GroupVersionKind]resourcesemantics.GenericCRD{
	servingv1alpha1.SchemeGroupVersion.WithKind("DomainMapping"): &servingv1alpha1.DomainMapping{},
	servingv1beta1.SchemeGroupVersion.WithKind("DomainMapping"):  &servingv1beta1.DomainMapping{},
}

func newDefaultingAdmissionController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	// Decorate contexts with the current state of the config.
	store := config.NewStore(ctx)
	store.WatchConfigs(cmw)

	return defaulting.NewAdmissionController(ctx,

		// Name of the resource webhook.
		"webhook.domainmapping.serving.knative.dev",

		// The path on which to serve the webhook.
		"/defaulting",

		// The resources to default.
		types,

		// A function that infuses the context passed to Validate/SetDefaults with custom metadata.
		store.ToContext,

		// Whether to disallow unknown fields. We set this to 'false' since
		// our CRDs have schemas
		false,
	)
}

func newValidatingAdmissionController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	// Decorate contexts with the current state of the config.
	store := config.NewStore(ctx)
	store.WatchConfigs(cmw)

	return validation.NewAdmissionController(ctx,

		// Name of the resource webhook.
		"validation.webhook.domainmapping.serving.knative.dev",

		// The path on which to serve the webhook.
		"/resource-validation",

		// The resources to validate.
		types,

		// A function that infuses the context passed to Validate/SetDefaults with custom metadata.
		store.ToContext,

		// Whether to disallow unknown fields. We set this to 'false' since
		// our CRDs have schemas
		false,
	)
}

func main() {
	flag.DurationVar(&reconciler.DefaultTimeout,
		"reconciliation-timeout", reconciler.DefaultTimeout,
		"The amount of time to give each reconciliation of a resource to complete before its context is canceled.")

	labelName := networking.ServingCertName + secretLabelNamePostfix
	ctx := filteredFactory.WithSelectors(signals.NewContext(), labelName)
	ctx = sharedmain.WithHealthProbesDisabled(ctx)

	// Set up webhook options
	ctx = webhook.WithOptions(ctx, webhook.Options{
		ServiceName: "controller",
		Port:        webhook.PortFromEnv(8443),
		SecretName:  "domainmapping-webhook-certs",
	})

	sharedmain.MainWithContext(ctx, "controller", ctors...)
}
