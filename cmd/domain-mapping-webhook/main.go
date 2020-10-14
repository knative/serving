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
	"knative.dev/pkg/webhook/resourcesemantics/validation"
	servingv1alpha1 "knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/reconciler/domainmapping/config"
)

var types = map[schema.GroupVersionKind]resourcesemantics.GenericCRD{
	servingv1alpha1.SchemeGroupVersion.WithKind("DomainMapping"): &servingv1alpha1.DomainMapping{},
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

		// Whether to disallow unknown fields.
		true,
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

		// Whether to disallow unknown fields.
		true,
	)
}

func main() {
	// Set up a signal context with our webhook options
	ctx := webhook.WithOptions(signals.NewContext(), webhook.Options{
		ServiceName: "domainmapping-webhook",
		Port:        webhook.PortFromEnv(8443),
		SecretName:  "domainmapping-webhook-certs",
	})

	sharedmain.WebhookMainWithContext(ctx, "domainmapping-webhook",
		certificates.NewController,
		newDefaultingAdmissionController,
		newValidatingAdmissionController,
	)
}
