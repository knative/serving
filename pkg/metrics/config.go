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

package metrics

import (
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
)

const (
	defaultLogURLTemplate = "http://localhost:8001/api/v1/namespaces/knative-monitoring/services/kibana-logging/proxy/app/kibana#/discover?_a=(query:(match:(kubernetes.labels.knative-dev%2FrevisionUID:(query:'${REVISION_UID}',type:phrase))))"
)

// ObservabilityConfig contains the configuration defined in the observability ConfigMap.
type ObservabilityConfig struct {
	// EnableVarLogCollection specifies whether the logs under /var/log/ should be available
	// for collection on the host node by the fluentd daemon set.
	EnableVarLogCollection bool

	// LoggingURLTemplate is a string containing the logging url template where
	// the variable REVISION_UID will be replaced with the created revision's UID.
	LoggingURLTemplate string

	// RequestLogTemplate is the go template to use to shape the request logs.
	RequestLogTemplate string

	// RequestMetricsBackend specifies the request metrics destination, e.g. Prometheus,
	// Stackdriver.
	RequestMetricsBackend string

	// EnableProfiling indicates whether it is allowed to retrieve runtime profiling data from
	// the pods via an HTTP server in the format expected by the pprof visualization tool.
	EnableProfiling bool
}

// NewObservabilityConfigFromConfigMap creates a ObservabilityConfig from the supplied ConfigMap
func NewObservabilityConfigFromConfigMap(configMap *corev1.ConfigMap) (*ObservabilityConfig, error) {
	oc := &ObservabilityConfig{}
	if evlc, ok := configMap.Data["logging.enable-var-log-collection"]; ok {
		oc.EnableVarLogCollection = strings.ToLower(evlc) == "true"
	}

	if rut, ok := configMap.Data["logging.revision-url-template"]; ok {
		oc.LoggingURLTemplate = rut
	} else {
		oc.LoggingURLTemplate = defaultLogURLTemplate
	}

	if rlt, ok := configMap.Data["logging.request-log-template"]; ok {
		// Verify that we get valid templates.
		if _, err := template.New("requestLog").Parse(rlt); err != nil {
			return nil, err
		}
		oc.RequestLogTemplate = rlt
	}

	if mb, ok := configMap.Data["metrics.request-metrics-backend-destination"]; ok {
		oc.RequestMetricsBackend = mb
	}

	if prof, ok := configMap.Data["profiling.enable"]; ok {
		oc.EnableProfiling = strings.ToLower(prof) == "true"
	}

	return oc, nil
}
