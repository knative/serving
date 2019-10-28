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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	DomainEnv        = "METRICS_DOMAIN"
	ConfigMapNameEnv = "CONFIG_OBSERVABILITY_NAME"
)

// metricsBackend specifies the backend to use for metrics
type metricsBackend string

const (
	// The following keys are used to configure metrics reporting.
	// See https://github.com/knative/serving/blob/master/config/config-observability.yaml
	// for details.
	AllowStackdriverCustomMetricsKey    = "metrics.allow-stackdriver-custom-metrics"
	BackendDestinationKey               = "metrics.backend-destination"
	ReportingPeriodKey                  = "metrics.reporting-period-seconds"
	StackdriverCustomMetricSubDomainKey = "metrics.stackdriver-custom-metrics-subdomain"
	// Stackdriver client configuration keys
	stackdriverProjectIDKey          = "metrics.stackdriver-project-id"
	stackdriverGCPLocationKey        = "metrics.stackdriver-gcp-location"
	stackdriverClusterNameKey        = "metrics.stackdriver-cluster-name"
	stackdriverGCPSecretNameKey      = "metrics.stackdriver-gcp-secret-name"
	stackdriverGCPSecretNamespaceKey = "metrics.stackdriver-gcp-secret-namespace"

	// Stackdriver is used for Stackdriver backend
	Stackdriver metricsBackend = "stackdriver"
	// Prometheus is used for Prometheus backend
	Prometheus metricsBackend = "prometheus"

	defaultBackendEnvName = "DEFAULT_METRICS_BACKEND"

	defaultPrometheusPort = 9090
	maxPrometheusPort     = 65535
	minPrometheusPort     = 1024
)

type metricsConfig struct {
	// The metrics domain. e.g. "serving.knative.dev" or "build.knative.dev".
	domain string
	// The component that emits the metrics. e.g. "activator", "autoscaler".
	component string
	// The metrics backend destination.
	backendDestination metricsBackend
	// reportingPeriod specifies the interval between reporting aggregated views.
	// If duration is less than or equal to zero, it enables the default behavior.
	reportingPeriod time.Duration

	// ---- Prometheus specific below ----
	// prometheusPort is the port where metrics are exposed in Prometheus
	// format. It defaults to 9090.
	prometheusPort int

	// ---- Stackdriver specific below ----
	// allowStackdriverCustomMetrics indicates whether it is allowed to send metrics to
	// Stackdriver using "global" resource type and custom metric type if the
	// metrics are not supported by the registered monitored resource types. Setting this
	// flag to "true" could cause extra Stackdriver charge.
	// If backendDestination is not Stackdriver, this is ignored.
	allowStackdriverCustomMetrics bool
	// stackdriverCustomMetricsSubDomain is the subdomain to use when sending custom metrics to StackDriver.
	// If not specified, the default is `knative.dev`.
	// If backendDestination is not Stackdriver, this is ignored.
	stackdriverCustomMetricsSubDomain string
	// True if backendDestination equals to "stackdriver". Store this in a variable
	// to reduce string comparison operations.
	isStackdriverBackend bool
	// stackdriverMetricTypePrefix is the metric domain joins component, e.g.
	// "knative.dev/serving/activator". Store this in a variable to reduce string
	// join operations.
	stackdriverMetricTypePrefix string
	// stackdriverCustomMetricTypePrefix is "custom.googleapis.com" joined with the subdomain and component.
	// E.g., "custom.googleapis.com/<subdomain>/<component>".
	// Store this in a variable to reduce string join operations.
	stackdriverCustomMetricTypePrefix string
	// stackdriverClientConfig is the metadata to configure the metrics exporter's Stackdriver client.
	stackdriverClientConfig stackdriverClientConfig
}

// stackdriverClientConfig encapsulates the metadata required to configure a Stackdriver client.
type stackdriverClientConfig struct {
	// ProjectID is the stackdriver project ID to which data is uploaded.
	// This is not necessarily the GCP project ID where the Kubernetes cluster is hosted.
	// Required when the Kubernetes cluster is not hosted on GCE.
	ProjectID string
	// GCPLocation is the GCP region or zone to which data is uploaded.
	// This is not necessarily the GCP location where the Kubernetes cluster is hosted.
	// Required when the Kubernetes cluster is not hosted on GCE.
	GCPLocation string
	// ClusterName is the cluster name with which the data will be associated in Stackdriver.
	// Required when the Kubernetes cluster is not hosted on GCE.
	ClusterName string
	// GCPSecretName is the optional GCP service account key which will be used to
	// authenticate with Stackdriver. If not provided, Google Application Default Credentials
	// will be used (https://cloud.google.com/docs/authentication/production).
	GCPSecretName string
	// GCPSecretNamespace is the Kubernetes namespace where GCPSecretName is located.
	// The Kubernetes ServiceAccount used by the pod that is exporting data to
	// Stackdriver should have access to Secrets in this namespace.
	GCPSecretNamespace string
}

// newStackdriverClientConfigFromMap creates a stackdriverClientConfig from the given map
func newStackdriverClientConfigFromMap(config map[string]string) *stackdriverClientConfig {
	return &stackdriverClientConfig{
		ProjectID:          config[stackdriverProjectIDKey],
		GCPLocation:        config[stackdriverGCPLocationKey],
		ClusterName:        config[stackdriverClusterNameKey],
		GCPSecretName:      config[stackdriverGCPSecretNameKey],
		GCPSecretNamespace: config[stackdriverGCPSecretNamespaceKey],
	}
}

func createMetricsConfig(ops ExporterOptions, logger *zap.SugaredLogger) (*metricsConfig, error) {
	var mc metricsConfig

	if ops.Domain == "" {
		return nil, errors.New("metrics domain cannot be empty")
	}
	mc.domain = ops.Domain

	if ops.Component == "" {
		return nil, errors.New("metrics component name cannot be empty")
	}
	mc.component = ops.Component

	if ops.ConfigMap == nil {
		return nil, errors.New("metrics config map cannot be empty")
	}
	m := ops.ConfigMap
	// Read backend setting from environment variable first
	backend := os.Getenv(defaultBackendEnvName)
	if backend == "" {
		// Use Prometheus if DEFAULT_METRICS_BACKEND does not exist or is empty
		backend = string(Prometheus)
	}
	// Override backend if it is setting in config map.
	if backendFromConfig, ok := m[BackendDestinationKey]; ok {
		backend = backendFromConfig
	}
	lb := metricsBackend(strings.ToLower(backend))
	switch lb {
	case Stackdriver, Prometheus:
		mc.backendDestination = lb
	default:
		return nil, fmt.Errorf("unsupported metrics backend value %q", backend)
	}

	if mc.backendDestination == Prometheus {
		pp := ops.PrometheusPort
		if pp == 0 {
			pp = defaultPrometheusPort
		}
		if pp < minPrometheusPort || pp > maxPrometheusPort {
			return nil, fmt.Errorf("invalid port %v, should between %v and %v", pp, minPrometheusPort, maxPrometheusPort)
		}
		mc.prometheusPort = pp
	}

	// If stackdriverClientConfig is not provided for stackdriver backend destination, OpenCensus will try to
	// use the application default credentials. If that is not available, Opencensus would fail to create the
	// metrics exporter.
	if mc.backendDestination == Stackdriver {
		scc := newStackdriverClientConfigFromMap(m)
		mc.stackdriverClientConfig = *scc
		mc.isStackdriverBackend = true
		mc.stackdriverMetricTypePrefix = path.Join(mc.domain, mc.component)

		mc.stackdriverCustomMetricsSubDomain = defaultCustomMetricSubDomain
		if sdcmd, ok := m[StackdriverCustomMetricSubDomainKey]; ok && sdcmd != "" {
			mc.stackdriverCustomMetricsSubDomain = sdcmd
		}
		mc.stackdriverCustomMetricTypePrefix = path.Join(customMetricTypePrefix, mc.stackdriverCustomMetricsSubDomain, mc.component)
		if ascmStr, ok := m[AllowStackdriverCustomMetricsKey]; ok && ascmStr != "" {
			ascmBool, err := strconv.ParseBool(ascmStr)
			if err != nil {
				return nil, fmt.Errorf("invalid %s value %q", AllowStackdriverCustomMetricsKey, ascmStr)
			}
			mc.allowStackdriverCustomMetrics = ascmBool
		}
	}

	// If reporting period is specified, use the value from the configuration.
	// If not, set a default value based on the selected backend.
	// Each exporter makes different promises about what the lowest supported
	// reporting period is. For Stackdriver, this value is 1 minute.
	// For Prometheus, we will use a lower value since the exporter doesn't
	// push anything but just responds to pull requests, and shorter durations
	// do not really hurt the performance and we rely on the scraping configuration.
	if repStr, ok := m[ReportingPeriodKey]; ok && repStr != "" {
		repInt, err := strconv.Atoi(repStr)
		if err != nil {
			return nil, fmt.Errorf("invalid %s value %q", ReportingPeriodKey, repStr)
		}
		mc.reportingPeriod = time.Duration(repInt) * time.Second
	} else if mc.backendDestination == Stackdriver {
		mc.reportingPeriod = 60 * time.Second
	} else if mc.backendDestination == Prometheus {
		mc.reportingPeriod = 5 * time.Second
	}

	return &mc, nil
}

// ConfigMapName gets the name of the metrics ConfigMap
func ConfigMapName() string {
	cm := os.Getenv(ConfigMapNameEnv)
	if cm == "" {
		return "config-observability"
	}
	return cm
}

// Domain holds the metrics domain to use for surfacing metrics.
func Domain() string {
	if domain := os.Getenv(DomainEnv); domain != "" {
		return domain
	}

	panic(fmt.Sprintf(`The environment variable %q is not set

If this is a process running on Kubernetes, then it should be specifying
this via:

  env:
  - name: %s
    value: knative.dev/some-repository

If this is a Go unit test consuming metric.Domain() then it should add the
following import:

import (
	_ "knative.dev/pkg/metrics/testing"
)`, DomainEnv, DomainEnv))
}

// JsonToMetricsOptions converts a json string of a
// ExporterOptions. Returns a non-nil ExporterOptions always.
func JsonToMetricsOptions(jsonOpts string) (*ExporterOptions, error) {
	var opts ExporterOptions
	if jsonOpts == "" {
		return nil, errors.New("json options string is empty")
	}

	if err := json.Unmarshal([]byte(jsonOpts), &opts); err != nil {
		return nil, err
	}

	return &opts, nil
}

// MetricsOptionsToJson converts a ExporterOptions to a json string.
func MetricsOptionsToJson(opts *ExporterOptions) (string, error) {
	if opts == nil {
		return "", nil
	}

	jsonOpts, err := json.Marshal(opts)
	if err != nil {
		return "", err
	}

	return string(jsonOpts), nil
}
