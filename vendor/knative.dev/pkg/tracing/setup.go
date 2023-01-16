/*
Copyright 2020 The Knative Authors

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

package tracing

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/tracing/config"
)

type Tracer interface {
	// Shutdown allows for proper cleanup/flush.
	Shutdown(ctx context.Context) error
}

// SetupPublishingWithStaticConfig sets up trace publishing for the process. The caller should call .Shutdown() on
// the returned Tracer before shutdown to make sure all traces are properly flushed.
// Note that other pieces still need to generate the traces, this just ensures that if generated, they are collected
// appropriately. This is normally done by using tracing.HTTPSpanMiddleware as a middleware HTTP
// handler. The configuration will not be dynamically updated.
func SetupPublishingWithStaticConfig(logger *zap.SugaredLogger, serviceName string, cfg *config.Config) (Tracer, error) {
	oct := NewOpenCensusTracer(WithExporter(serviceName, logger))
	if err := oct.ApplyConfig(cfg); err != nil {
		return oct, fmt.Errorf("unable to set OpenCensusTracing config: %w", err)
	}
	return oct, nil
}

// Deprecated: Use SetupPublishingWithStaticConfig.
func SetupStaticPublishing(logger *zap.SugaredLogger, serviceName string, cfg *config.Config) error {
	_, err := SetupPublishingWithStaticConfig(logger, serviceName, cfg)
	return err
}

// SetupPublishingWithDynamicConfig sets up trace publishing for the process, by watching a
// ConfigMap for the configuration. The caller should call .Shutdown() on
// the returned OpenCensusTracer before shutdown to make sure all traces are properly flushed.
// Note that other pieces still need to generate the traces, this
// just ensures that if generated, they are collected appropriately. This is normally done by using
// tracing.HTTPSpanMiddleware as a middleware HTTP handler. The configuration will be dynamically
// updated when the ConfigMap is updated.
func SetupPublishingWithDynamicConfig(logger *zap.SugaredLogger, configMapWatcher configmap.Watcher, serviceName, tracingConfigName string) (Tracer, error) {
	oct := NewOpenCensusTracer(WithExporter(serviceName, logger))

	tracerUpdater := func(name string, value interface{}) {
		if name == tracingConfigName {
			cfg := value.(*config.Config)
			logger.Debugw("Updating tracing config", zap.Any("cfg", cfg))
			if err := oct.ApplyConfig(cfg); err != nil {
				logger.Errorw("Unable to apply open census tracer config", zap.Error(err))
			}
		}
	}

	// Set up our config store.
	configStore := configmap.NewUntypedStore(
		"config-tracing-store",
		logger,
		configmap.Constructors{
			"config-tracing": config.NewTracingConfigFromConfigMap,
		},
		tracerUpdater)
	configStore.WatchConfigs(configMapWatcher)
	return oct, nil
}

// Deprecated: Use SetupPublishingWithDynamicConfig.
func SetupDynamicPublishing(logger *zap.SugaredLogger, configMapWatcher configmap.Watcher, serviceName, tracingConfigName string) error {
	_, err := SetupPublishingWithDynamicConfig(logger, configMapWatcher, serviceName, tracingConfigName)
	return err
}

// SetupPublishingWithDynamicConfigAndInitialValue sets up the trace publishing for the process with an
// initial value, by watching a ConfigMap for the configuration. Note that other pieces still
// need to generate the traces, this just ensures that if generated, they are collected
// appropriately. This is normally done by using tracing.HTTPSpanMiddleware as a middleware
// HTTP handler. The configuration will be dynamically updated when the ConfigMap is updated.
func SetupPublishingWithDynamicConfigAndInitialValue(logger *zap.SugaredLogger, configMapWatcher configmap.Watcher, serviceName string, configm *corev1.ConfigMap) (Tracer, error) {
	oct := NewOpenCensusTracer(WithExporter(serviceName, logger))
	cfg, err := config.NewTracingConfigFromConfigMap(configm)
	if err != nil {
		return oct, fmt.Errorf("error while parsing %s config map: %w", configm.Name, err)
	}
	if err = oct.ApplyConfig(cfg); err != nil {
		return oct, fmt.Errorf("unable to set OpenCensusTracing config: %w", err)
	}

	tracerUpdater := func(name string, value interface{}) {
		if name == configm.Name {
			cfg := value.(*config.Config)
			logger.Debugw("Updating tracing config", zap.Any("cfg", cfg))
			if err := oct.ApplyConfig(cfg); err != nil {
				logger.Errorw("Unable to apply open census tracer config", zap.Error(err))
			}
		}
	}

	// Set up our config store.
	configStore := configmap.NewUntypedStore(
		"config-tracing-store",
		logger,
		configmap.Constructors{
			"config-tracing": config.NewTracingConfigFromConfigMap,
		},
		tracerUpdater)
	configStore.WatchConfigs(configMapWatcher)
	return oct, nil
}
