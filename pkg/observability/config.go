/*
Copyright 2025 The Knative Authors

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

package observability

import (
	"context"
	"fmt"
	texttemplate "text/template"

	configmap "knative.dev/pkg/configmap/parser"
	pkgo11y "knative.dev/pkg/observability"
	metrics "knative.dev/pkg/observability/metrics"
)

const (
	// DefaultLogURLTemplate is used to set the default log url template
	DefaultLogURLTemplate = ""

	// DefaultRequestLogTemplate is the default format for emitting request logs.
	DefaultRequestLogTemplate = `{"httpRequest": {"requestMethod": "{{.Request.Method}}", "requestUrl": "{{js .Request.RequestURI}}", "requestSize": "{{.Request.ContentLength}}", "status": {{.Response.Code}}, "responseSize": "{{.Response.Size}}", "userAgent": "{{js .Request.UserAgent}}", "remoteIp": "{{js .Request.RemoteAddr}}", "serverIp": "{{.Revision.PodIP}}", "referer": "{{js .Request.Referer}}", "latency": "{{.Response.Latency}}s", "protocol": "{{.Request.Proto}}"}, "traceId": "{{.TraceID}}"}`

	// ReqLogTemplateKey is the CM key for the request log template.
	RequestLogTemplateKey = "logging.request-log-template"

	// EnableReqLogKey is the CM key to enable request log.
	EnableRequestLogKey = "logging.enable-request-log"

	// EnableProbeReqLogKey is the CM key to enable request logs for probe requests.
	EnableProbeRequestLogKey = "logging.enable-probe-request-log"
)

type (
	BaseConfig    = pkgo11y.Config
	MetricsConfig = pkgo11y.MetricsConfig
	RuntimeConfig = pkgo11y.RuntimeConfig
	TracingConfig = pkgo11y.TracingConfig
)

// +k8s:deepcopy-gen=true
type Config struct {
	BaseConfig

	RequestMetrics MetricsConfig `json:"requestMetrics"`

	// EnableVarLogCollection specifies whether the logs under /var/log/ should be available
	// for collection on the host node by the fluentd daemon set.
	//
	// We elide this value from JSON serialization because it's not relevant for
	// the queue proxy
	EnableVarLogCollection bool `json:",omitempty"`

	// LoggingURLTemplate is a string containing the logging url template where
	// the variable REVISION_UID will be replaced with the created revision's UID.
	//
	// We elide this value from JSON serialization because it's not relevant for
	// the queue proxy
	LoggingURLTemplate string `json:",omitempty"`

	// EnableRequestLog enables activator/queue-proxy to write request logs.
	EnableRequestLog bool `json:"enableRequestLog,omitempty"`

	// RequestLogTemplate is the go template to use to shape the request logs.
	RequestLogTemplate string `json:"requestLogTemplate,omitempty"`

	// EnableProbeRequestLog enables queue-proxy to write health check probe request logs.
	EnableProbeRequestLog bool `json:"enableProbeRequestLog,omitempty"`
}

func (c *Config) Validate() error {
	if c.RequestLogTemplate == "" && c.EnableRequestLog {
		return fmt.Errorf("%q was set to true, but no %q was specified", EnableRequestLogKey, RequestLogTemplateKey)
	}

	if c.RequestLogTemplate != "" {
		// Verify that we get valid templates.
		if _, err := texttemplate.New("requestLog").Parse(c.RequestLogTemplate); err != nil {
			return err
		}
	}
	return nil
}

func DefaultConfig() *Config {
	return &Config{
		BaseConfig:         *pkgo11y.DefaultConfig(),
		RequestMetrics:     metrics.DefaultConfig(),
		LoggingURLTemplate: DefaultLogURLTemplate,
		RequestLogTemplate: DefaultRequestLogTemplate,
	}
}

func NewFromMap(m map[string]string) (*Config, error) {
	c := DefaultConfig()

	if cfg, err := pkgo11y.NewFromMap(m); err != nil {
		return nil, err
	} else {
		c.BaseConfig = *cfg
	}

	if rm, err := metrics.NewFromMapWithPrefix("request-", m); err != nil {
		return nil, err
	} else {
		c.RequestMetrics = rm
	}

	err := configmap.Parse(m,
		configmap.As("logging.enable-var-log-collection", &c.EnableVarLogCollection),
		configmap.As("logging.revision-url-template", &c.LoggingURLTemplate),
		configmap.As(RequestLogTemplateKey, &c.RequestLogTemplate),
		configmap.As(EnableRequestLogKey, &c.EnableRequestLog),
		configmap.As(EnableProbeRequestLogKey, &c.EnableProbeRequestLog),
	)
	if err != nil {
		return c, err
	}

	return c, c.Validate()
}

type cfgKey struct{}

// WithConfig associates a observability configuration with the context.
func WithConfig(ctx context.Context, cfg *Config) context.Context {
	return context.WithValue(ctx, cfgKey{}, cfg)
}

// GetConfig gets the observability config from the provided context.
func GetConfig(ctx context.Context) *Config {
	untyped := ctx.Value(cfgKey{})
	if untyped == nil {
		return nil
	}
	return untyped.(*Config)
}
