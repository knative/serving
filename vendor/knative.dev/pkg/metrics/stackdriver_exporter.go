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
	"fmt"
	"path"
	"sync"

	"contrib.go.opencensus.io/exporter/stackdriver"
	"contrib.go.opencensus.io/exporter/stackdriver/monitoredresource"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.uber.org/zap"
	"google.golang.org/api/option"
	"knative.dev/pkg/metrics/metricskey"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	customMetricTypePrefix = "custom.googleapis.com"
	// defaultCustomMetricSubDomain is the default subdomain to use for unsupported metrics by monitored resource types.
	// See: https://cloud.google.com/monitoring/api/ref_v3/rest/v3/projects.metricDescriptors#MetricDescriptor
	defaultCustomMetricSubDomain = "knative.dev"
	// secretNamespaceDefault is the namespace to search for a k8s Secret to pass to Stackdriver client to authenticate with Stackdriver.
	secretNamespaceDefault = "default"
	// secretDataFieldKey is the name of the k8s Secret field that contains the Secret's key.
	secretDataFieldKey = "key.json"
)

var (
	// kubeclient is the in-cluster Kubernetes kubeclient, which is lazy-initialized on first use.
	kubeclient *kubernetes.Clientset
	// initClientOnce is the lazy initializer for kubeclient.
	initClientOnce sync.Once
	// kubeclientInitErr capture an error during initClientOnce
	kubeclientInitErr error
)

func init() {
	kubeclientInitErr = nil
}

func newOpencensusSDExporter(o stackdriver.Options) (view.Exporter, error) {
	return stackdriver.NewExporter(o)
}

// TODO should be properly refactored to be able to inject the getMonitoredResourceFunc function.
// 	See https://github.com/knative/pkg/issues/608
func newStackdriverExporter(config *metricsConfig, logger *zap.SugaredLogger) (view.Exporter, error) {
	gm := getMergedGCPMetadata(config)
	mtf := getMetricTypeFunc(config.stackdriverMetricTypePrefix, config.stackdriverCustomMetricTypePrefix)
	co, err := getStackdriverExporterClientOptions(&config.stackdriverClientConfig)
	if err != nil {
		logger.Warnw("Issue configuring Stackdriver exporter client options, no additional client options will be used: ", zap.Error(err))
	}
	// Automatically fall back on Google application default credentials
	e, err := newOpencensusSDExporter(stackdriver.Options{
		ProjectID:               gm.project,
		Location:                gm.location,
		MonitoringClientOptions: co,
		TraceClientOptions:      co,
		GetMetricDisplayName:    mtf, // Use metric type for display name for custom metrics. No impact on built-in metrics.
		GetMetricType:           mtf,
		GetMonitoredResource:    getMonitoredResourceFunc(config.stackdriverMetricTypePrefix, gm),
		DefaultMonitoringLabels: &stackdriver.Labels{},
	})
	if err != nil {
		logger.Errorw("Failed to create the Stackdriver exporter: ", zap.Error(err))
		return nil, err
	}
	logger.Infof("Created Opencensus Stackdriver exporter with config %v", config)
	return e, nil
}

// getStackdriverExporterClientOptions creates client options for the opencensus Stackdriver exporter from the given stackdriverClientConfig.
// On error, an empty array of client options is returned.
func getStackdriverExporterClientOptions(sdconfig *stackdriverClientConfig) ([]option.ClientOption, error) {
	var co []option.ClientOption
	if sdconfig.GCPSecretName != "" {
		secret, err := getStackdriverSecret(sdconfig)
		if err != nil {
			return co, err
		}

		co = append(co, convertSecretToExporterOption(secret))
	}

	return co, nil
}

// getMergedGCPMetadata returns GCP metadata required to export metrics
// to Stackdriver. Values can come from the GCE metadata server or the config.
//  Values explicitly set in the config take the highest precedent.
func getMergedGCPMetadata(config *metricsConfig) *gcpMetadata {
	gm := retrieveGCPMetadata()
	if config.stackdriverClientConfig.ProjectID != "" {
		gm.project = config.stackdriverClientConfig.ProjectID
	}

	if config.stackdriverClientConfig.GCPLocation != "" {
		gm.location = config.stackdriverClientConfig.GCPLocation
	}

	if config.stackdriverClientConfig.ClusterName != "" {
		gm.cluster = config.stackdriverClientConfig.ClusterName
	}

	return gm
}

func getMonitoredResourceFunc(metricTypePrefix string, gm *gcpMetadata) func(v *view.View, tags []tag.Tag) ([]tag.Tag, monitoredresource.Interface) {
	return func(view *view.View, tags []tag.Tag) ([]tag.Tag, monitoredresource.Interface) {
		metricType := path.Join(metricTypePrefix, view.Measure.Name())
		if metricskey.KnativeRevisionMetrics.Has(metricType) {
			return GetKnativeRevisionMonitoredResource(view, tags, gm)
		} else if metricskey.KnativeBrokerMetrics.Has(metricType) {
			return GetKnativeBrokerMonitoredResource(view, tags, gm)
		} else if metricskey.KnativeTriggerMetrics.Has(metricType) {
			return GetKnativeTriggerMonitoredResource(view, tags, gm)
		} else if metricskey.KnativeSourceMetrics.Has(metricType) {
			return GetKnativeSourceMonitoredResource(view, tags, gm)
		}
		// Unsupported metric by knative_revision, knative_broker, knative_trigger, and knative_source, use "global" resource type.
		return getGlobalMonitoredResource(view, tags)
	}
}

func getGlobalMonitoredResource(v *view.View, tags []tag.Tag) ([]tag.Tag, monitoredresource.Interface) {
	return tags, &Global{}
}

func getMetricTypeFunc(metricTypePrefix, customMetricTypePrefix string) func(view *view.View) string {
	return func(view *view.View) string {
		metricType := path.Join(metricTypePrefix, view.Measure.Name())
		inServing := metricskey.KnativeRevisionMetrics.Has(metricType)
		inEventing := metricskey.KnativeBrokerMetrics.Has(metricType) ||
			metricskey.KnativeTriggerMetrics.Has(metricType) ||
			metricskey.KnativeSourceMetrics.Has(metricType)
		if inServing || inEventing {
			return metricType
		}
		// Unsupported metric by knative_revision, use custom domain.
		return path.Join(customMetricTypePrefix, view.Measure.Name())
	}
}

// getStackdriverSecret returns the Kubernetes Secret specified in the given config.
func getStackdriverSecret(sdconfig *stackdriverClientConfig) (*corev1.Secret, error) {
	if err := ensureKubeclient(); err != nil {
		return nil, err
	}

	ns := sdconfig.GCPSecretNamespace
	if ns == "" {
		ns = secretNamespaceDefault
	}

	sec, secErr := kubeclient.CoreV1().Secrets(ns).Get(sdconfig.GCPSecretName, metav1.GetOptions{})

	if secErr != nil {
		return nil, fmt.Errorf("Error getting Secret [%v] in namespace [%v]: %v", sdconfig.GCPSecretName, sdconfig.GCPSecretNamespace, secErr)
	}

	return sec, nil
}

// convertSecretToExporterOption converts a Kubernetes Secret to an OpenCensus Stackdriver Exporter Option.
func convertSecretToExporterOption(secret *corev1.Secret) option.ClientOption {
	return option.WithCredentialsJSON(secret.Data[secretDataFieldKey])
}

// ensureKubeclient is the lazy initializer for kubeclient.
func ensureKubeclient() error {
	// initClientOnce is only run once and cannot return error, so kubeclientInitErr is used to capture errors.
	initClientOnce.Do(func() {
		config, err := rest.InClusterConfig()
		if err != nil {
			kubeclientInitErr = err
			return
		}

		cs, err := kubernetes.NewForConfig(config)
		if err != nil {
			kubeclientInitErr = err
			return
		}
		kubeclient = cs
	})

	return kubeclientInitErr
}
