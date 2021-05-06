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

	"os"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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

	ctx := signals.NewContext()

	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")

	if projectID == "" {
		panic("no google cloud project env var")
	}

	exporter, err := texporter.NewExporter(texporter.WithProjectID(projectID))

	if err != nil {
		panic("texporter.NewExporter: " + err.Error())
	}
	defer exporter.Shutdown(ctx) // flushes any pending spans

	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	sharedmain.MainWithContext(ctx, "controller", ctors...)
}
