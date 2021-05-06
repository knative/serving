package main

import (
	"context"
	"log"
	"os"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")

	if projectID == "" {
		panic("google project id empty")
	}
	// Create exporter and trace provider pipeline, and register provider.
	_, shutdown, err := texporter.InstallNewPipeline(
		[]texporter.Option{
			texporter.WithProjectID(projectID),
		},
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		// other optional provider options
	)
	if err != nil {
		log.Fatalf("texporter.InstallNewPipeline: %v", err)
	}
	// before ending program, wait for all enqueued spans to be exported
	defer shutdown()

	// Create custom span.
	tracer := otel.GetTracerProvider().Tracer("example.com/trace")
	err = func(ctx context.Context) error {
		ctx, span := tracer.Start(ctx, "foo")
		defer span.End()

		// Do some work.

		return nil
	}(context.Background())
}
