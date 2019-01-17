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
	"fmt"
	"net/http"
	"sync"

	"contrib.go.opencensus.io/exporter/stackdriver"
	"contrib.go.opencensus.io/exporter/stackdriver/monitoredresource"
	"github.com/knative/pkg/metrics/metricskey"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.uber.org/zap"
)

var (
	curMetricsExporter       view.Exporter
	curMetricsConfig         *metricsConfig
	curPromSrv               *http.Server
	getMonitoredResourceFunc func(v *view.View, tags []tag.Tag) ([]tag.Tag, monitoredresource.Interface)
	metricsMux               sync.Mutex
)

// newMetricsExporter gets a metrics exporter based on the config.
func newMetricsExporter(config *metricsConfig, logger *zap.SugaredLogger) error {
	// If there is a Prometheus Exporter server running, stop it.
	resetCurPromSrv()
	ce := getCurMetricsExporter()
	if ce != nil {
		// UnregisterExporter is idempotent and it can be called multiple times for the same exporter
		// without side effects.
		view.UnregisterExporter(ce)
	}
	var err error
	var e view.Exporter
	switch config.backendDestination {
	case Stackdriver:
		e, err = newStackdriverExporter(config, logger)
	case Prometheus:
		e, err = newPrometheusExporter(config, logger)
	default:
		err = fmt.Errorf("Unsupported metrics backend %v", config.backendDestination)
	}
	if err != nil {
		return err
	}
	existingConfig := getCurMetricsConfig()
	setCurMetricsExporterAndConfig(e, config)
	logger.Infof("Successfully updated the metrics exporter; old config: %v; new config %v", existingConfig, config)
	return nil
}

func getKnativeRevisionMonitoredResource(gm *gcpMetadata) func(v *view.View, tags []tag.Tag) ([]tag.Tag, monitoredresource.Interface) {
	return func(v *view.View, tags []tag.Tag) ([]tag.Tag, monitoredresource.Interface) {
		tagsMap := getTagsMap(tags)
		kr := &KnativeRevision{
			// The first three resource labels are from metadata.
			Project:     gm.project,
			Location:    gm.location,
			ClusterName: gm.cluster,
			// The rest resource labels are from metrics labels.
			NamespaceName:     valueOrUnknown(metricskey.LabelNamespaceName, tagsMap),
			ServiceName:       valueOrUnknown(metricskey.LabelServiceName, tagsMap),
			ConfigurationName: valueOrUnknown(metricskey.LabelConfigurationName, tagsMap),
			RevisionName:      valueOrUnknown(metricskey.LabelRevisionName, tagsMap),
		}

		var newTags []tag.Tag
		for _, t := range tags {
			// Keep the metrics labels that are not resource labels
			if _, ok := metricskey.KnativeRevisionLabels[t.Key.Name()]; !ok {
				newTags = append(newTags, t)
			}
		}

		return newTags, kr
	}
}

func getTagsMap(tags []tag.Tag) map[string]string {
	tagsMap := map[string]string{}
	for _, t := range tags {
		tagsMap[t.Key.Name()] = t.Value
	}
	return tagsMap
}

func valueOrUnknown(key string, tagsMap map[string]string) string {
	if value, ok := tagsMap[key]; ok {
		return value
	}
	return metricskey.ValueUnknown
}

func getGlobalMonitoredResource() func(v *view.View, tags []tag.Tag) ([]tag.Tag, monitoredresource.Interface) {
	return func(v *view.View, tags []tag.Tag) ([]tag.Tag, monitoredresource.Interface) {
		return tags, &Global{}
	}
}

func newStackdriverExporter(config *metricsConfig, logger *zap.SugaredLogger) (view.Exporter, error) {
	setMonitoredResourceFunc(config, logger)
	e, err := stackdriver.NewExporter(stackdriver.Options{
		ProjectID:               config.stackdriverProjectID,
		MetricPrefix:            config.domain + "/" + config.component,
		GetMonitoredResource:    getMonitoredResourceFunc,
		DefaultMonitoringLabels: &stackdriver.Labels{},
	})
	if err != nil {
		logger.Error("Failed to create the Stackdriver exporter: ", zap.Error(err))
		return nil, err
	}
	logger.Infof("Created Opencensus Stackdriver exporter with config %v", config)
	return e, nil
}

func newPrometheusExporter(config *metricsConfig, logger *zap.SugaredLogger) (view.Exporter, error) {
	e, err := prometheus.NewExporter(prometheus.Options{Namespace: config.component})
	if err != nil {
		logger.Error("Failed to create the Prometheus exporter.", zap.Error(err))
		return nil, err
	}
	logger.Infof("Created Opencensus Prometheus exporter with config: %v. Start the server for Prometheus exporter.", config)
	// Start the server for Prometheus scraping
	go func() {
		srv := startNewPromSrv(e)
		srv.ListenAndServe()
	}()
	return e, nil
}

func getCurPromSrv() *http.Server {
	metricsMux.Lock()
	defer metricsMux.Unlock()
	return curPromSrv
}

func resetCurPromSrv() {
	metricsMux.Lock()
	defer metricsMux.Unlock()
	if curPromSrv != nil {
		curPromSrv.Close()
		curPromSrv = nil
	}
}

func resetMonitoredResourceFunc() {
	metricsMux.Lock()
	defer metricsMux.Unlock()
	if getMonitoredResourceFunc != nil {
		getMonitoredResourceFunc = nil
	}
}

func setMonitoredResourceFunc(config *metricsConfig, logger *zap.SugaredLogger) {
	metricsMux.Lock()
	defer metricsMux.Unlock()
	if getMonitoredResourceFunc == nil {
		gm := retrieveGCPMetadata()
		metricsPrefix := config.domain + "/" + config.component
		logger.Infof("metrics prefix: %s", metricsPrefix)
		if _, ok := metricskey.KnativeRevisionMetricsPrefixes[metricsPrefix]; ok {
			getMonitoredResourceFunc = getKnativeRevisionMonitoredResource(gm)
		} else {
			getMonitoredResourceFunc = getGlobalMonitoredResource()
		}
	}
}

func startNewPromSrv(e *prometheus.Exporter) *http.Server {
	sm := http.NewServeMux()
	sm.Handle("/metrics", e)
	metricsMux.Lock()
	defer metricsMux.Unlock()
	if curPromSrv != nil {
		curPromSrv.Close()
	}
	curPromSrv = &http.Server{
		Addr:    ":9090",
		Handler: sm,
	}
	return curPromSrv
}

func getCurMetricsExporter() view.Exporter {
	metricsMux.Lock()
	defer metricsMux.Unlock()
	return curMetricsExporter
}

func setCurMetricsExporterAndConfig(e view.Exporter, c *metricsConfig) {
	metricsMux.Lock()
	defer metricsMux.Unlock()
	view.RegisterExporter(e)
	if c != nil {
		view.SetReportingPeriod(c.reportingPeriod)
	} else {
		// Setting to 0 enables the default behavior.
		view.SetReportingPeriod(0)
	}
	curMetricsExporter = e
	curMetricsConfig = c
}

func getCurMetricsConfig() *metricsConfig {
	metricsMux.Lock()
	defer metricsMux.Unlock()
	return curMetricsConfig
}
