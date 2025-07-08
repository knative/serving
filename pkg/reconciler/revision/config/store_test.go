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
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"

	network "knative.dev/networking/pkg"
	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/logging"
	logtesting "knative.dev/pkg/logging/testing"
	apiconfig "knative.dev/serving/pkg/apis/config"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/deployment"
	"knative.dev/serving/pkg/observability"
	o11yconfigmap "knative.dev/serving/pkg/observability/configmap"

	. "knative.dev/pkg/configmap/testing"
)

func TestStoreLoadWithContext(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	deploymentConfig := ConfigMapFromTestFile(t, deployment.ConfigName, deployment.QueueSidecarImageKey)
	networkConfig := ConfigMapFromTestFile(t, netcfg.ConfigMapName)
	observabilityConfig, observabilityConfigExample := ConfigMapsFromTestFile(t, o11yconfigmap.Name())
	loggingConfig, loggingConfigExample := ConfigMapsFromTestFile(t, logging.ConfigMapName())
	defaultConfig := ConfigMapFromTestFile(t, apiconfig.DefaultsConfigName)
	autoscalerConfig := ConfigMapFromTestFile(t, autoscalerconfig.ConfigName)
	featuresConfig := ConfigMapFromTestFile(t, apiconfig.FeaturesConfigName)

	watcher := configmap.NewStaticWatcher(
		featuresConfig,
		deploymentConfig,
		networkConfig,
		observabilityConfig,
		loggingConfig,
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
		expected, _ := o11yconfigmap.Parse(observabilityConfig)
		if diff := cmp.Diff(expected, config.Observability); diff != "" {
			t.Error("Unexpected observability config (-want, +got):", diff)
		}

		// Default config.
		want, _ := o11yconfigmap.Parse(&corev1.ConfigMap{Data: map[string]string{}})
		got, err := o11yconfigmap.Parse(observabilityConfigExample)
		if err != nil {
			t.Fatal("Error parsing example observability config:", err)
		}

		// Compare with the example and allow the log url template to differ
		co := cmpopts.IgnoreFields(observability.Config{},
			"BaseConfig",
			"RequestMetrics",
			"LoggingURLTemplate")
		if !cmp.Equal(got, want, co) {
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
		ConfigMapFromTestFile(t, netcfg.ConfigMapName),
		ConfigMapFromTestFile(t, o11yconfigmap.Name()),
		ConfigMapFromTestFile(t, logging.ConfigMapName()),
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
