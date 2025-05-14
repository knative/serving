/*
Copyright 2022 The Knative Authors

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

package tracingotel

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/serving/pkg/tracingotel/config"
)

// TracingHandle defines an interface for something that can be shut down.
// This is returned by setup functions to allow callers to shut down tracing.
// TODO: Consider if this interface is still needed long-term or if callers should
// simply ensure tracing.Shutdown() is called once globally.
// For now, returning an object that calls the global tracing.Shutdown() maintains contract.
type TracingHandle interface {
	// Shutdown allows for proper cleanup/flush of the global tracing provider.
	Shutdown(ctx context.Context) error
}

// otelTracerHandle is a simple struct that implements the TracingHandle interface
// by calling the global OpenTelemetry shutdown function.
type otelTracerHandle struct{}

// Shutdown calls the global tracing.Shutdown function from otel.go.
func (t *otelTracerHandle) Shutdown(ctx context.Context) error {
	return Shutdown(ctx) // This calls otel.go's Shutdown
}

// SetupPublishingWithStaticConfig sets up trace publishing for the process using OpenTelemetry.
// The configuration is static and applied once.
// The caller should call .Shutdown() on the returned TracingHandle before process exit.
func SetupPublishingWithStaticConfig(logger *zap.SugaredLogger, serviceName string, cfg *config.Config) (TracingHandle, error) {
	// Init from otel.go will configure the global OpenTelemetry provider.
	if err := Init(serviceName, cfg, logger); err != nil {
		return nil, fmt.Errorf("failed to initialize OpenTelemetry tracing with static config: %w", err)
	}
	return &otelTracerHandle{}, nil
}

// Deprecated: Use SetupPublishingWithStaticConfig.
func SetupStaticPublishing(logger *zap.SugaredLogger, serviceName string, cfg *config.Config) error {
	_, err := SetupPublishingWithStaticConfig(logger, serviceName, cfg)
	return err
}

// SetupPublishingWithDynamicConfig sets up trace publishing using OpenTelemetry,
// by watching a ConfigMap for the configuration.
// Tracing will be re-initialized if the tracing configuration changes.
// Note: Re-initializing OpenTelemetry can be disruptive and may lead to brief data loss.
// The caller should call .Shutdown() on the returned TracingHandle before process exit to shutdown the last active tracer.
func SetupPublishingWithDynamicConfig(logger *zap.SugaredLogger, configMapWatcher configmap.Watcher, serviceName, tracingConfigName string) (TracingHandle, error) {
	// Initial setup with a default/empty config. Assumes if config-tracing doesn't exist yet,
	// it might result in a no-op tracer until the config map appears.
	// Or, Init could be called by the watcher for the first time.
	// For safety, let's initialize with a default "disabled" config if no specific initial config is given.
	initialCfg := &config.Config{Backend: config.None} // Default to no tracing initially
	if err := Init(serviceName, initialCfg, logger); err != nil {
		logger.Warnw("Initial OpenTelemetry tracing setup failed (defaulted to None), will retry on config update.", zap.Error(err))
		// Continue, as the watcher might fix this.
	}

	tracerUpdater := func(name string, value interface{}) {
		if name == tracingConfigName {
			cfg, ok := value.(*config.Config)
			if !ok {
				logger.Errorw("Invalid config type in tracerUpdater", zap.String("name", name), zap.Any("value", value))
				return
			}
			logger.Infow("Attempting to update OpenTelemetry tracing config", zap.Any("newConfig", cfg))
			if err := Init(serviceName, cfg, logger); err != nil {
				logger.Errorw("Failed to re-initialize OpenTelemetry tracing on dynamic config update", zap.Error(err))
			} else {
				logger.Infow("Successfully re-initialized OpenTelemetry tracing with new config")
			}
		}
	}

	// Set up our config store.
	configStore := configmap.NewUntypedStore(
		"otel-config-tracing-store", // Use a different name to avoid conflict if old OC store existed
		logger,
		configmap.Constructors{
			tracingConfigName: config.NewTracingConfigFromConfigMap, // Assumes config-tracing name is passed in tracingConfigName
		},
		tracerUpdater)
	configStore.WatchConfigs(configMapWatcher)

	return &otelTracerHandle{}, nil
}

// Deprecated: Use SetupPublishingWithDynamicConfig.
func SetupDynamicPublishing(logger *zap.SugaredLogger, configMapWatcher configmap.Watcher, serviceName, tracingConfigName string) error {
	_, err := SetupPublishingWithDynamicConfig(logger, configMapWatcher, serviceName, tracingConfigName)
	return err
}

// SetupPublishingWithDynamicConfigAndInitialValue sets up trace publishing using OpenTelemetry with an initial value,
// and watches a ConfigMap for subsequent configurations.
// Tracing will be re-initialized if the tracing configuration changes.
// Note: Re-initializing OpenTelemetry can be disruptive and may lead to brief data loss.
func SetupPublishingWithDynamicConfigAndInitialValue(logger *zap.SugaredLogger, configMapWatcher configmap.Watcher, serviceName string, initialConfigMap *corev1.ConfigMap) (TracingHandle, error) {
	cfg, err := config.NewTracingConfigFromConfigMap(initialConfigMap)
	if err != nil {
		return nil, fmt.Errorf("error parsing initial tracing ConfigMap %s: %w", initialConfigMap.Name, err)
	}

	if err := Init(serviceName, cfg, logger); err != nil {
		return nil, fmt.Errorf("failed to initialize OpenTelemetry tracing with initial config from %s: %w", initialConfigMap.Name, err)
	}

	// Name of the configmap key to watch, should match the one initialConfigMap came from.
	// This assumes initialConfigMap.Name is the key for config.NewTracingConfigFromConfigMap constructor.
	// If the configmap name in K8s is different from the key in configStore, this needs care.
	// Typically, tracingConfigName (like "config-tracing") is the key.
	tracingConfigName := initialConfigMap.Name

	tracerUpdater := func(name string, value interface{}) {
		if name == tracingConfigName {
			updatedCfg, ok := value.(*config.Config)
			if !ok {
				logger.Errorw("Invalid config type in tracerUpdater", zap.String("name", name), zap.Any("value", value))
				return
			}
			logger.Infow("Attempting to update OpenTelemetry tracing config dynamically", zap.Any("newConfig", updatedCfg))
			if err := Init(serviceName, updatedCfg, logger); err != nil {
				logger.Errorw("Failed to re-initialize OpenTelemetry tracing on dynamic config update", zap.Error(err))
			} else {
				logger.Infow("Successfully re-initialized OpenTelemetry tracing with new config")
			}
		}
	}

	configStore := configmap.NewUntypedStore(
		"otel-config-tracing-store-initial",
		logger,
		configmap.Constructors{
			tracingConfigName: config.NewTracingConfigFromConfigMap,
		},
		tracerUpdater)
	configStore.WatchConfigs(configMapWatcher)

	return &otelTracerHandle{}, nil
}
