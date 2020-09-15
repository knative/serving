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

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/system"

	. "knative.dev/pkg/configmap/testing"
	_ "knative.dev/pkg/system/testing"
)

func TestOurObservability(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, metrics.ConfigMapName())

	realCfg, err := metrics.NewObservabilityConfigFromConfigMap(cm)
	if err != nil {
		t.Fatal("NewObservabilityConfigFromConfigMap(actual) =", err)
	}
	if realCfg == nil {
		t.Fatal("NewObservabilityConfigFromConfigMap(actual) = nil")
	}

	exCfg, err := metrics.NewObservabilityConfigFromConfigMap(example)
	if err != nil {
		t.Fatal("NewObservabilityConfigFromConfigMap(example) =", err)
	}
	if exCfg == nil {
		t.Fatal("NewObservabilityConfigFromConfigMap(example) = nil")
	}

	if !cmp.Equal(realCfg, exCfg) {
		t.Errorf("actual != example: diff(-actual,+exCfg):\n%s", cmp.Diff(realCfg, exCfg))
	}
}

func TestObservabilityConfiguration(t *testing.T) {
	observabilityConfigTests := []struct {
		name       string
		wantErr    bool
		wantConfig *metrics.ObservabilityConfig
		config     *corev1.ConfigMap
	}{{
		name: "observability configuration with all inputs",
		wantConfig: &metrics.ObservabilityConfig{
			EnableProbeRequestLog:  true,
			EnableRequestLog:       true,
			EnableVarLogCollection: true,
			LoggingURLTemplate:     "https://logging.io",
			RequestLogTemplate:     `{"requestMethod": "{{.Request.Method}}"}`,
			RequestMetricsBackend:  "stackdriver",
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      metrics.ConfigMapName(),
			},
			Data: map[string]string{
				"logging.enable-probe-request-log":            "true",
				metrics.EnableReqLogKey:                       "true",
				"logging.enable-var-log-collection":           "true",
				metrics.ReqLogTemplateKey:                     `{"requestMethod": "{{.Request.Method}}"}`,
				"logging.revision-url-template":               "https://logging.io",
				"logging.write-request-logs":                  "true",
				"metrics.request-metrics-backend-destination": "stackdriver",
			},
		},
	}, {
		name: "observability configuration with default template but request logging isn't on",
		wantConfig: &metrics.ObservabilityConfig{
			EnableProbeRequestLog:  true,
			EnableVarLogCollection: true,
			LoggingURLTemplate:     "https://logging.io",
			RequestLogTemplate:     metrics.DefaultRequestLogTemplate,
			RequestMetricsBackend:  "stackdriver",
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      metrics.ConfigMapName(),
			},
			Data: map[string]string{
				"logging.enable-probe-request-log":            "true",
				metrics.EnableReqLogKey:                       "false",
				"logging.enable-var-log-collection":           "true",
				"logging.revision-url-template":               "https://logging.io",
				"logging.write-request-logs":                  "true",
				"metrics.request-metrics-backend-destination": "stackdriver",
			},
		},
	}, {
		name: "observability config with no map",
		wantConfig: &metrics.ObservabilityConfig{
			LoggingURLTemplate:    metrics.DefaultLogURLTemplate,
			RequestLogTemplate:    metrics.DefaultRequestLogTemplate,
			RequestMetricsBackend: "prometheus",
		},
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      metrics.ConfigMapName(),
			},
		},
	}, {
		name:    "invalid request log template",
		wantErr: true,
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      metrics.ConfigMapName(),
			},
			Data: map[string]string{
				metrics.ReqLogTemplateKey: `{{ something }}`,
			},
		},
	}}

	for _, tt := range observabilityConfigTests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := metrics.NewObservabilityConfigFromConfigMap(tt.config)

			if (err != nil) != tt.wantErr {
				t.Fatalf("NewObservabilityFromConfigMap() error = %v, WantErr %v", err, tt.wantErr)
			}

			if got, want := actual, tt.wantConfig; !cmp.Equal(want, got) {
				t.Fatalf("Got %#v, want: %#v; diff(-want,+got):\n%s", got, want, cmp.Diff(want, got))
			}
		})
	}
}
