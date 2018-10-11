/*
Copyright 2018 The Knative Authors.
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

package metrics

import (
	"testing"
	"time"

	. "github.com/knative/pkg/logging/testing"
)

const (
	proj = "test-project"
)

func TestNewStackdriverExporter(t *testing.T) {
	// The stackdriver project ID is required for stackdriver exporter.
	exporter, err := newStackdriverExporter(metricsConfig{"", "", proj}, TestLogger(t))
	if err != nil {
		t.Error(err)
	}
	if exporter == nil {
		t.Error("expected a non-nil metrics exporter")
	}

	exporter, err = newStackdriverExporter(metricsConfig{"", "", ""}, TestLogger(t))
	if err == nil {
		t.Error("expected an error if the project id is empty")
	}
	if exporter != nil {
		t.Error("expected a nil metrics exporter")
	}
}

func TestNewPrometheusExporter(t *testing.T) {
	// The stackdriver project ID is not required for prometheus exporter.
	_, err := newPrometheusExporter(metricsConfig{"", "", ""}, TestLogger(t))
	if err != nil {
		t.Error(err)
	}
	time.Sleep(10 * time.Millisecond)
	if promSrv == nil {
		t.Error("expected a server for prometheus exporter")
	}
}

func TestInterlevedExporters(t *testing.T) {
	// First create a stackdriver exporter
	_, err := newMetricsExporter(metricsConfig{
		component:            "",
		backendDestination:   Stackdriver,
		stackdriverProjectId: proj}, TestLogger(t))
	if err != nil {
		t.Error(err)
	}
	if promSrv != nil {
		t.Error("expected no server for stackdriver exporter")
	}
	// Then switch to prometheus exporter
	_, err = newMetricsExporter(metricsConfig{
		component:            "",
		backendDestination:   Prometheus,
		stackdriverProjectId: "",
	}, TestLogger(t))
	if err != nil {
		t.Error(err)
	}
	time.Sleep(10 * time.Millisecond)
	if promSrv == nil {
		t.Error("expected a server for prometheus exporter")
	}
	// Finally switch to stackdriver exporter
	_, err = newMetricsExporter(metricsConfig{
		component:            "",
		backendDestination:   Stackdriver,
		stackdriverProjectId: proj}, TestLogger(t))
	if err != nil {
		t.Error(err)
	}
	if promSrv != nil {
		t.Error("expected no server for stackdriver exporter")
	}
}

func TestGetMetricsConfig(t *testing.T) {
	_, err := getMetricsConfig(map[string]string{
		"": "",
	}, "component", TestLogger(t))
	if err == nil {
		t.Error("expected an error")
	}
	if err.Error() != "metrics.backend-destination key is missing" {
		t.Errorf("expected error: metrics.backend-destination key is missing. Got %v", err)
	}

	c, err := getMetricsConfig(map[string]string{
		"metrics.backend-destination": "prometheus",
	}, "component", TestLogger(t))
	if err != nil {
		t.Error("failed to get config for prometheus")
	}
	expected := metricsConfig{
		component:            "component",
		backendDestination:   Prometheus,
		stackdriverProjectId: "",
	}
	if c != expected {
		t.Errorf("expected config: %v; got config: %v", expected, c)
	}

	_, err = getMetricsConfig(map[string]string{
		"metrics.backend-destination": "stackdriver",
	}, "component", TestLogger(t))
	if err.Error() != "metrics.stackdriver-project-id key is missing when the backend-destination is set to stackdriver." {
		t.Errorf("expected error: metrics.stackdriver-project-id key is missing when the backend-destination is set to stackdriver. Got %v", err)
	}

	c, err = getMetricsConfig(map[string]string{
		"metrics.backend-destination":    "stackdriver",
		"metrics.stackdriver-project-id": proj,
	}, "component", TestLogger(t))
	if err != nil {
		t.Error("failed to get config for stackdriver")
	}
	expected = metricsConfig{
		component:            "component",
		backendDestination:   Stackdriver,
		stackdriverProjectId: proj,
	}
	if c != expected {
		t.Errorf("expected config: %v; got config: %v", expected, c)
	}

	c, err = getMetricsConfig(map[string]string{
		"metrics.backend-destination": "unsupported",
	}, "component", TestLogger(t))
	if err != nil {
		t.Error("failed to get config for stackdriver")
	}
	expected = metricsConfig{
		component:            "component",
		backendDestination:   Prometheus,
		stackdriverProjectId: "",
	}
	if c != expected {
		t.Errorf("expected config: %v; got config: %v", expected, c)
	}
}
