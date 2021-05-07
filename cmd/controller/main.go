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

	"os"

	"knative.dev/serving/pkg/reconciler/configuration"
	"knative.dev/serving/pkg/reconciler/gc"
	"knative.dev/serving/pkg/reconciler/labeler"
	"knative.dev/serving/pkg/reconciler/revision"
	"knative.dev/serving/pkg/reconciler/route"
	"knative.dev/serving/pkg/reconciler/serverlessservice"
	"knative.dev/serving/pkg/reconciler/service"

	// This defines the shared main for injected controllers.
	"knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv"
	"knative.dev/pkg/signals"
)

var ctors = []injection.ControllerConstructor{
	configuration.NewController,
	labeler.NewController,
	revision.NewController,
	route.NewController,
	serverlessservice.NewController,
	service.NewController,
	gc.NewController,
}

func main() {
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if projectID == "" {
		panic("no google cloud project env var")
	}
	// Create exporter and trace provider pipeline, and register provider.
	_, shutdown, err := texporter.InstallNewPipeline(
		[]texporter.Option{
			// optional exporter options
			texporter.WithProjectID(projectID),
		},
		// This example code uses sdktrace.AlwaysSample sampler to sample all traces.
		// In a production environment or high QPS setup please use ProbabilitySampler
		// set at the desired probability.
		// Example:
		// sdktrace.WithConfig(sdktrace.Config {
		//     DefaultSampler: sdktrace.ProbabilitySampler(0.0001),
		// })
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.ServiceNameKey.String("controller"),
			semconv.ServiceInstanceIDKey.String(os.Getenv("POD_NAME")),
			semconv.K8SPodNameKey.String(os.Getenv("POD_NAME")),
		)),
		// other optional provider options
	)

	if err != nil {
		panic("texporter.NewExporter: " + err.Error())
	}

	defer shutdown()

	ctx := signals.NewContext()
	sharedmain.MainWithContext(ctx, "controller", ctors...)
}
