module github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace

go 1.14

require (
	cloud.google.com/go v0.61.0
	github.com/golang/protobuf v1.4.2
	github.com/googleinterns/cloud-operations-api-mock v0.0.0-20200709193332-a1e58c29bdd3
	github.com/stretchr/testify v1.7.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.20.0
	go.opentelemetry.io/otel v0.20.0
	go.opentelemetry.io/otel/sdk v0.20.0
	go.opentelemetry.io/otel/trace v0.20.0
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d
	google.golang.org/api v0.29.0
	google.golang.org/genproto v0.0.0-20200715011427-11fb19a81f2c
	google.golang.org/grpc v1.32.0
	google.golang.org/protobuf v1.25.0
)
