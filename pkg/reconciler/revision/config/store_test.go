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

package config

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"

	network "knative.dev/networking/pkg"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/logging"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/metrics"
	pkgtracing "knative.dev/pkg/tracing/config"
	apiconfig "knative.dev/serving/pkg/apis/config"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/deployment"

	. "knative.dev/pkg/configmap/testing"
)

func TestStoreLoadWithContext(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	deploymentConfig := ConfigMapFromTestFile(t, deployment.ConfigName, deployment.QueueSidecarImageKey)
	networkConfig := ConfigMapFromTestFile(t, network.ConfigName)
	observabilityConfig, observabilityConfigExample := ConfigMapsFromTestFile(t, metrics.ConfigMapName())
	loggingConfig, loggingConfigExample := ConfigMapsFromTestFile(t, logging.ConfigMapName())
	tracingConfig, tracingConfigExample := ConfigMapsFromTestFile(t, pkgtracing.ConfigName)
	defaultConfig := ConfigMapFromTestFile(t, apiconfig.DefaultsConfigName)
	autoscalerConfig := ConfigMapFromTestFile(t, autoscalerconfig.ConfigName)
	featuresConfig := ConfigMapFromTestFile(t, apiconfig.FeaturesConfigName)

	watcher := configmap.NewStaticWatcher(
		featuresConfig,
		deploymentConfig,
		networkConfig,
		observabilityConfig,
		loggingConfig,
		tracingConfig,
		defaultConfig,
		autoscalerConfig)

	store.WatchConfigs(watcher)

	config := FromContext(store.ToContext(context.Background()))

	t.Run("Deployment", func(t *testing.T) {
		expected, _ := deployment.NewConfigFromConfigMap(deploymentConfig)
		if diff := cmp.Diff(expected, config.Deployment); diff != "" {
			t.Error("Unexpected deployment (-want, +got):", diff)
		}
	})

	t.Run("network", func(t *testing.T) {
		expected, _ := network.NewConfigFromConfigMap(networkConfig)
		if diff := cmp.Diff(expected, config.Network); diff != "" {
			t.Error("Unexpected controller config (-want, +got):", diff)
		}
	})

	t.Run("observability", func(t *testing.T) {
		expected, _ := metrics.NewObservabilityConfigFromConfigMap(observabilityConfig)
		if diff := cmp.Diff(expected, config.Observability); diff != "" {
			t.Error("Unexpected observability config (-want, +got):", diff)
		}

		// Default config.
		want, _ := metrics.NewObservabilityConfigFromConfigMap(&corev1.ConfigMap{Data: map[string]string{}})
		got, err := metrics.NewObservabilityConfigFromConfigMap(observabilityConfigExample)
		if err != nil {
			t.Fatal("Error parsing example observability config:", err)
		}
		if !cmp.Equal(got, want) {
			t.Error("Example Observability Config does not match the default, diff(-want,+got):\n", cmp.Diff(want, got))
		}
	})

	t.Run("logging", func(t *testing.T) {
		expected, _ := logging.NewConfigFromConfigMap(loggingConfig)
		if diff := cmp.Diff(expected, config.Logging); diff != "" {
			t.Error("Unexpected logging config (-want, +got):", diff)
		}

		// Default config.
		want, _ := logging.NewConfigFromConfigMap(&corev1.ConfigMap{Data: map[string]string{}})
		got, err := logging.NewConfigFromConfigMap(loggingConfigExample)
		if err != nil {
			t.Fatal("Error parsing example logging config:", err)
		}
		if cmp.Equal(got, want) {
			t.Error("Example Logging Config does not match the default, diff(-want,+got):\n", cmp.Diff(want, got))
		}
	})

	t.Run("tracing", func(t *testing.T) {
		expected, _ := pkgtracing.NewTracingConfigFromConfigMap(tracingConfig)
		if diff := cmp.Diff(expected, config.Tracing); diff != "" {
			t.Error("Unexpected tracing config (-want, +got):", diff)
		}

		// Default config.
		want, _ := pkgtracing.NewTracingConfigFromConfigMap(&corev1.ConfigMap{Data: map[string]string{}})
		got, err := pkgtracing.NewTracingConfigFromConfigMap(tracingConfigExample)
		if err != nil {
			t.Fatal("Error parsing example tracing config:", err)
		}
		if cmp.Equal(got, want) {
			t.Error("Example Tracing Config does not match the default, diff(-want,+got):\n", cmp.Diff(want, got))
		}
	})

	t.Run("defaults", func(t *testing.T) {
		expected, _ := apiconfig.NewDefaultsConfigFromConfigMap(defaultConfig)
		if diff := cmp.Diff(expected, config.Defaults); diff != "" {
			t.Error("Unexpected defaults config (-want, +got):", diff)
		}
	})

	t.Run("autoscaler", func(t *testing.T) {
		expected, _ := autoscalerconfig.NewConfigFromConfigMap(autoscalerConfig)
		if diff := cmp.Diff(expected, config.Autoscaler); diff != "" {
			t.Error("Unexpected autoscaler config (-want, +got):", diff)
		}
	})

	t.Run("features", func(t *testing.T) {
		expected, _ := apiconfig.NewFeaturesConfigFromConfigMap(featuresConfig)
		if diff := cmp.Diff(expected, config.Features); diff != "" {
			t.Error("Unexpected autoscaler config (-want, +got):", diff)
		}
	})
}

func TestStoreImmutableConfig(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))
	watcher := configmap.NewStaticWatcher(
		ConfigMapFromTestFile(t, deployment.ConfigName, deployment.QueueSidecarImageKey),
		ConfigMapFromTestFile(t, network.ConfigName),
		ConfigMapFromTestFile(t, metrics.ConfigMapName()),
		ConfigMapFromTestFile(t, logging.ConfigMapName()),
		ConfigMapFromTestFile(t, pkgtracing.ConfigName),
		ConfigMapFromTestFile(t, apiconfig.DefaultsConfigName),
		ConfigMapFromTestFile(t, autoscalerconfig.ConfigName),
		ConfigMapFromTestFile(t, apiconfig.FeaturesConfigName),
	)

	store.WatchConfigs(watcher)

	config := store.Load()

	config.Deployment.QueueSidecarImage = "mutated"
	config.Logging.LoggingConfig = "mutated"
	ccMutated := int64(4)
	config.Defaults.ContainerConcurrency = ccMutated
	scaleupMutated := float64(4)
	config.Autoscaler.MaxScaleUpRate = scaleupMutated

	newConfig := store.Load()

	if newConfig.Deployment.QueueSidecarImage == "mutated" {
		t.Error("Controller config is not immutable")
	}
	if newConfig.Logging.LoggingConfig == "mutated" {
		t.Error("Logging config is not immutable")
	}
	if newConfig.Defaults.ContainerConcurrency == ccMutated {
		t.Error("Defaults config is not immutable")
	}
	if newConfig.Autoscaler.MaxScaleUpRate == scaleupMutated {
		t.Error("Autoscaler config is not immutable")
	}
}
