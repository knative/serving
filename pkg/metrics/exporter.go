/*
Copyright 2019 The Knative Authors

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
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	pkgmetrics "knative.dev/pkg/metrics"
)

// UpdateExporterFromConfigMap sets metrics config map fields that should be uniform for knative/serving components
// and then calls through to UpdateExporterFromConfigMap from "knative.dev/pkg/metrics".
func UpdateExporterFromConfigMap(component string, logger *zap.SugaredLogger) func(configMap *corev1.ConfigMap) {
	return func(configMap *corev1.ConfigMap) {
		if configMap.Data["metrics.stackdriver-gcp-secret-name"] != "" {
			configMap.Data["metrics.stackdriver-gcp-secret-name"] = "stackdriver-service-account-key"
			configMap.Data["metrics.stackdriver-gcp-secret-namespace"] = "knative-serving"
		}
		pkgmetrics.UpdateExporterFromConfigMap(component, logger)(configMap)
	}
}
