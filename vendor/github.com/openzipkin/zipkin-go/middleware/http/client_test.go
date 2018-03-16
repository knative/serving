package http_test

import (
	"net/http"
	"testing"

	zipkin "github.com/openzipkin/zipkin-go"
	httpclient "github.com/openzipkin/zipkin-go/middleware/http"
	"github.com/openzipkin/zipkin-go/reporter/recorder"
)

func TestHTTPClient(t *testing.T) {
	reporter := recorder.NewReporter()
	defer reporter.Close()

	ep, _ := zipkin.NewEndpoint("httpClient", "")
	tracer, err := zipkin.NewTracer(reporter, zipkin.WithLocalEndpoint(ep))
	if err != nil {
		t.Fatalf("unable to create tracer: %+v", err)
	}

	clientTags := map[string]string{
		"client": "testClient",
	}

	transportTags := map[string]string{
		"conf.timeout": "default",
	}

	client, err := httpclient.NewClient(
		tracer,
		httpclient.WithClient(&http.Client{}),
		httpclient.ClientTrace(true),
		httpclient.ClientTags(clientTags),
		httpclient.TransportOptions(httpclient.TransportTags(transportTags)),
	)
	if err != nil {
		t.Fatalf("unable to create http client: %+v", err)
	}

	req, _ := http.NewRequest("GET", "https://www.google.com", nil)

	res, err := client.DoWithAppSpan(req, "Get Google")
	if err != nil {
		t.Fatalf("unable to execute client request: %+v", err)
	}
	res.Body.Close()

	spans := reporter.Flush()
	if len(spans) < 2 {
		t.Errorf("Span Count want 2+, have %d", len(spans))
	}

	req, _ = http.NewRequest("GET", "https://www.google.com", nil)

	res, err = client.Do(req)
	if err != nil {
		t.Fatalf("unable to execute client request: %+v", err)
	}
	res.Body.Close()

	spans = reporter.Flush()
	if len(spans) == 0 {
		t.Errorf("Span Count want 1+, have 0")
	}

	span := tracer.StartSpan("ParentSpan")

	req, _ = http.NewRequest("GET", "http://www.google.com", nil)

	ctx := zipkin.NewContext(req.Context(), span)

	req = req.WithContext(ctx)

	res, err = client.DoWithAppSpan(req, "ChildSpan")
	if err != nil {
		t.Fatalf("unable to execute client request: %+v", err)
	}
	res.Body.Close()

}
