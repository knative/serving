/*
Copyright 2025 The Knative Authors

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

package resource

import (
	"knative.dev/pkg/changeset"
	"knative.dev/pkg/system"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

// Default returns a default OpenTelemetry Resource enriched
// with common Knative/Kubernetes attributes.
//
// It will populate:
// - Namespace using system.Namespace
// - PodName using system.PodName
// - ServiceVersion with changeset.Get
func Default(serviceName string) *resource.Resource {
	attrs := []attribute.KeyValue{
		semconv.K8SNamespaceName(system.Namespace()),
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(changeset.Get()),
	}

	if pn := system.PodName(); pn != "" {
		attrs = append(attrs, semconv.K8SPodName(pn))
	}

	// Ignore the error because it complains about semconv
	// schema version differences
	resource, err := resource.Merge(
		resource.NewWithAttributes(
			semconv.SchemaURL,
			attrs...,
		),
		// We merge 'Default' last since this allows overriding
		// the service name etc. using env variables
		resource.Default(),
	)
	if err != nil {
		panic(err)
	}
	return resource
}
