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
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/leaderelection"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/webhook"
	"knative.dev/pkg/webhook/certificates"
	"knative.dev/pkg/webhook/configmaps"
	"knative.dev/pkg/webhook/resourcesemantics"
	"knative.dev/pkg/webhook/resourcesemantics/conversion"
	"knative.dev/pkg/webhook/resourcesemantics/defaulting"
	"knative.dev/pkg/webhook/resourcesemantics/validation"

	// resource validation types
	net "knative.dev/networking/pkg/apis/networking/v1alpha1"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	servingv1alpha1 "knative.dev/serving/pkg/apis/serving/v1alpha1"
	servingv1beta1 "knative.dev/serving/pkg/apis/serving/v1beta1"
	extravalidation "knative.dev/serving/pkg/webhook"

	// config validation constructors
	network "knative.dev/networking/pkg"
	tracingconfig "knative.dev/pkg/tracing/config"
	defaultconfig "knative.dev/serving/pkg/apis/config"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/gc"
	domainconfig "knative.dev/serving/pkg/reconciler/route/config"
)

var types = map[schema.GroupVersionKind]resourcesemantics.GenericCRD{
	servingv1alpha1.SchemeGroupVersion.WithKind("Revision"):      &servingv1alpha1.Revision{},
	servingv1alpha1.SchemeGroupVersion.WithKind("Configuration"): &servingv1alpha1.Configuration{},
	servingv1alpha1.SchemeGroupVersion.WithKind("Route"):         &servingv1alpha1.Route{},
	servingv1alpha1.SchemeGroupVersion.WithKind("Service"):       &servingv1alpha1.Service{},
	servingv1beta1.SchemeGroupVersion.WithKind("Revision"):       &servingv1beta1.Revision{},
	servingv1beta1.SchemeGroupVersion.WithKind("Configuration"):  &servingv1beta1.Configuration{},
	servingv1beta1.SchemeGroupVersion.WithKind("Route"):          &servingv1beta1.Route{},
	servingv1beta1.SchemeGroupVersion.WithKind("Service"):        &servingv1beta1.Service{},
	servingv1.SchemeGroupVersion.WithKind("Revision"):            &servingv1.Revision{},
	servingv1.SchemeGroupVersion.WithKind("Configuration"):       &servingv1.Configuration{},
	servingv1.SchemeGroupVersion.WithKind("Route"):               &servingv1.Route{},
	servingv1.SchemeGroupVersion.WithKind("Service"):             &servingv1.Service{},

	autoscalingv1alpha1.SchemeGroupVersion.WithKind("PodAutoscaler"): &autoscalingv1alpha1.PodAutoscaler{},
	autoscalingv1alpha1.SchemeGroupVersion.WithKind("Metric"):        &autoscalingv1alpha1.Metric{},

	net.SchemeGroupVersion.WithKind("Certificate"):       &net.Certificate{},
	net.SchemeGroupVersion.WithKind("Ingress"):           &net.Ingress{},
	net.SchemeGroupVersion.WithKind("ServerlessService"): &net.ServerlessService{},
}

var serviceValidation = validation.NewCallback(
	extravalidation.ValidateService, webhook.Create, webhook.Update)

var configValidation = validation.NewCallback(
	extravalidation.ValidateConfiguration, webhook.Create, webhook.Update)

var callbacks = map[schema.GroupVersionKind]validation.Callback{
	servingv1alpha1.SchemeGroupVersion.WithKind("Service"):       serviceValidation,
	servingv1beta1.SchemeGroupVersion.WithKind("Service"):        serviceValidation,
	servingv1.SchemeGroupVersion.WithKind("Service"):             serviceValidation,
	servingv1alpha1.SchemeGroupVersion.WithKind("Configuration"): configValidation,
	servingv1beta1.SchemeGroupVersion.WithKind("Configuration"):  configValidation,
	servingv1.SchemeGroupVersion.WithKind("Configuration"):       configValidation,
}

func newDefaultingAdmissionController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	// Decorate contexts with the current state of the config.
	store := defaultconfig.NewStore(logging.FromContext(ctx).Named("config-store"))
	store.WatchConfigs(cmw)

	return defaulting.NewAdmissionController(ctx,

		// Name of the resource webhook.
		"webhook.serving.knative.dev",

		// The path on which to serve the webhook.
		"/defaulting",

		// The resources to default.
		types,

		// A function that infuses the context passed to Validate/SetDefaults with custom metadata.
		func(ctx context.Context) context.Context {
			return servingv1.WithUpgradeViaDefaulting(store.ToContext(ctx))
		},

		// Whether to disallow unknown fields.
		true,
	)
}

func newValidationAdmissionController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	// Decorate contexts with the current state of the config.
	store := defaultconfig.NewStore(logging.FromContext(ctx).Named("config-store"))
	store.WatchConfigs(cmw)

	return validation.NewAdmissionController(ctx,

		// Name of the resource webhook.
		"validation.webhook.serving.knative.dev",

		// The path on which to serve the webhook.
		"/resource-validation",

		// The resources to validate.
		types,

		// A function that infuses the context passed to Validate/SetDefaults with custom metadata.
		func(ctx context.Context) context.Context {
			return servingv1.WithUpgradeViaDefaulting(store.ToContext(ctx))
		},

		// Whether to disallow unknown fields.
		true,

		// Extra validating callbacks to be applied to resources.
		callbacks,
	)
}

func newConfigValidationController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	return configmaps.NewAdmissionController(ctx,

		// Name of the configmap webhook.
		"config.webhook.serving.knative.dev",

		// The path on which to serve the webhook.
		"/config-validation",

		// The configmaps to validate.
		configmap.Constructors{
			tracingconfig.ConfigName:         tracingconfig.NewTracingConfigFromConfigMap,
			autoscalerconfig.ConfigName:      autoscalerconfig.NewConfigFromConfigMap,
			gc.ConfigName:                    gc.NewConfigFromConfigMapFunc(ctx),
			network.ConfigName:               network.NewConfigFromConfigMap,
			deployment.ConfigName:            deployment.NewConfigFromConfigMap,
			metrics.ConfigMapName():          metrics.NewObservabilityConfigFromConfigMap,
			logging.ConfigMapName():          logging.NewConfigFromConfigMap,
			leaderelection.ConfigMapName():   leaderelection.NewConfigFromConfigMap,
			domainconfig.DomainConfigName:    domainconfig.NewDomainFromConfigMap,
			defaultconfig.DefaultsConfigName: defaultconfig.NewDefaultsConfigFromConfigMap,
		},
	)
}

func newConversionController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	var (
		v1alpha1 = servingv1alpha1.SchemeGroupVersion.Version
		v1beta1  = servingv1beta1.SchemeGroupVersion.Version
		v1       = servingv1.SchemeGroupVersion.Version
	)

	return conversion.NewConversionController(ctx,
		// The path on which to serve the webhook
		"/resource-conversion",

		// Specify the types of custom resource definitions that should be converted
		map[schema.GroupKind]conversion.GroupKindConversion{
			servingv1.Kind("Service"): {
				DefinitionName: serving.ServicesResource.String(),
				HubVersion:     v1alpha1,
				Zygotes: map[string]conversion.ConvertibleObject{
					v1alpha1: &servingv1alpha1.Service{},
					v1beta1:  &servingv1beta1.Service{},
					v1:       &servingv1.Service{},
				},
			},
			servingv1.Kind("Configuration"): {
				DefinitionName: serving.ConfigurationsResource.String(),
				HubVersion:     v1alpha1,
				Zygotes: map[string]conversion.ConvertibleObject{
					v1alpha1: &servingv1alpha1.Configuration{},
					v1beta1:  &servingv1beta1.Configuration{},
					v1:       &servingv1.Configuration{},
				},
			},
			servingv1.Kind("Revision"): {
				DefinitionName: serving.RevisionsResource.String(),
				HubVersion:     v1alpha1,
				Zygotes: map[string]conversion.ConvertibleObject{
					v1alpha1: &servingv1alpha1.Revision{},
					v1beta1:  &servingv1beta1.Revision{},
					v1:       &servingv1.Revision{},
				},
			},
			servingv1.Kind("Route"): {
				DefinitionName: serving.RoutesResource.String(),
				HubVersion:     v1alpha1,
				Zygotes: map[string]conversion.ConvertibleObject{
					v1alpha1: &servingv1alpha1.Route{},
					v1beta1:  &servingv1beta1.Route{},
					v1:       &servingv1.Route{},
				},
			},
		},

		// A function that infuses the context passed to ConvertTo/ConvertFrom/SetDefaults with custom metadata.
		func(ctx context.Context) context.Context {
			return ctx
		},
	)
}

func main() {
	// Set up a signal context with our webhook options
	ctx := webhook.WithOptions(signals.NewContext(), webhook.Options{
		ServiceName: "webhook",
		Port:        webhook.PortFromEnv(8443),
		SecretName:  "webhook-certs",
	})

	sharedmain.WebhookMainWithContext(ctx, "webhook",
		certificates.NewController,
		newDefaultingAdmissionController,
		newValidationAdmissionController,
		newConfigValidationController,
		newConversionController,
	)
}
