/*
Copyright 2018 The Knative Authors.

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
	"math/rand"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/knative/pkg/configmap"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/logging"
	"go.uber.org/zap/zapcore"

	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

func TestStoreWatchConfigs(t *testing.T) {
	watcher := &mockWatcher{}

	store := Store{Logger: TestLogger(t)}
	store.WatchConfigs(watcher)

	want := []string{
		ControllerConfigName,
		NetworkConfigName,
		ObservabilityConfigName,
		logging.ConfigName,
		autoscaler.ConfigName,
	}

	got := watcher.watches

	if diff := cmp.Diff(want, got, sortStrings); diff != "" {
		t.Errorf("Unexpected configmap watches (-want, +got): %v", diff)
	}
}

func TestStoreLoadWithContext(t *testing.T) {
	store := Store{Logger: TestLogger(t)}

	controllerConfig := ConfigMapFromTestFile(t, ControllerConfigName)
	networkConfig := ConfigMapFromTestFile(t, NetworkConfigName)
	observabilityConfig := ConfigMapFromTestFile(t, ObservabilityConfigName)
	loggingConfig := ConfigMapFromTestFile(t, logging.ConfigName)
	autoscalerConfig := ConfigMapFromTestFile(t, autoscaler.ConfigName)

	store.setController(controllerConfig)
	store.setNetwork(networkConfig)
	store.setObservability(observabilityConfig)
	store.setLogging(loggingConfig)
	store.setAutoscaler(autoscalerConfig)

	config := FromContext(store.ToContext(context.Background()))

	t.Run("controller", func(t *testing.T) {
		expected, _ := NewControllerConfigFromConfigMap(controllerConfig)
		if diff := cmp.Diff(expected, config.Controller); diff != "" {
			t.Errorf("Unexpected controller config (-want, +got): %v", diff)
		}
	})

	t.Run("network", func(t *testing.T) {
		expected, _ := NewNetworkFromConfigMap(networkConfig)
		if diff := cmp.Diff(expected, config.Network); diff != "" {
			t.Errorf("Unexpected controller config (-want, +got): %v", diff)
		}
	})

	t.Run("observability", func(t *testing.T) {
		expected, _ := NewObservabilityFromConfigMap(observabilityConfig)
		if diff := cmp.Diff(expected, config.Observability); diff != "" {
			t.Errorf("Unexpected observability config (-want, +got): %v", diff)
		}
	})

	t.Run("logging", func(t *testing.T) {
		expected, _ := logging.NewConfigFromConfigMap(loggingConfig)
		if diff := cmp.Diff(expected, config.Logging); diff != "" {
			t.Errorf("Unexpected logging config (-want, +got): %v", diff)
		}
	})

	t.Run("autoscaler", func(t *testing.T) {
		expected, _ := autoscaler.NewConfigFromConfigMap(autoscalerConfig)
		if diff := cmp.Diff(expected, config.Autoscaler); diff != "" {
			t.Errorf("Unexpected autoscaler config (-want, +got): %v", diff)
		}
	})
}

func TestStoreImmutableConfig(t *testing.T) {
	store := Store{Logger: TestLogger(t)}

	store.setController(ConfigMapFromTestFile(t, ControllerConfigName))
	store.setNetwork(ConfigMapFromTestFile(t, NetworkConfigName))
	store.setObservability(ConfigMapFromTestFile(t, ObservabilityConfigName))
	store.setLogging(ConfigMapFromTestFile(t, logging.ConfigName))
	store.setAutoscaler(ConfigMapFromTestFile(t, autoscaler.ConfigName))

	config := store.Load()

	config.Controller.QueueSidecarImage = "mutated"
	config.Network.IstioOutboundIPRanges = "mutated"
	config.Observability.FluentdSidecarImage = "mutated"
	config.Logging.LoggingConfig = "mutated"
	config.Autoscaler.MaxScaleUpRate = rand.Float64()

	newConfig := store.Load()

	if newConfig.Controller.QueueSidecarImage == "mutated" {
		t.Error("Controller config is not immutable")
	}
	if newConfig.Network.IstioOutboundIPRanges == "mutated" {
		t.Error("Network config is not immutable")
	}
	if newConfig.Observability.FluentdSidecarImage == "mutated" {
		t.Error("Observability config is not immutable")
	}
	if newConfig.Logging.LoggingConfig == "mutated" {
		t.Error("Logging config is not immutabled")
	}
	if newConfig.Autoscaler.MaxScaleUpRate == config.Autoscaler.MaxScaleUpRate {
		t.Error("Autoscaler config is not immutabled")
	}
}

func TestStoreFailedUpdate(t *testing.T) {
	store := Store{Logger: TestLogger(t)}

	controllerConfig := ConfigMapFromTestFile(t, ControllerConfigName)
	networkConfig := ConfigMapFromTestFile(t, NetworkConfigName)
	observabilityConfig := ConfigMapFromTestFile(t, ObservabilityConfigName)
	loggingConfig := ConfigMapFromTestFile(t, logging.ConfigName)
	autoscalerConfig := ConfigMapFromTestFile(t, autoscaler.ConfigName)

	loggingConfig.Data["loglevel.controller"] = "debug"

	store.setController(controllerConfig)
	store.setNetwork(networkConfig)
	store.setObservability(observabilityConfig)
	store.setLogging(loggingConfig)
	store.setAutoscaler(autoscalerConfig)

	// Set a bad level which causes the update to fail
	loggingConfig.Data["loglevel.controller"] = "unknown"
	store.setLogging(loggingConfig)

	config := store.Load()
	if got, want := config.Logging.LoggingLevel["controller"], zapcore.DebugLevel; got != want {
		t.Errorf("Expected the update to fail - logging level want: %v, got: %v", want, got)
	}
}

type mockWatcher struct {
	watches []string
}

func (w *mockWatcher) Watch(config string, o configmap.Observer) {
	w.watches = append(w.watches, config)
}

func (*mockWatcher) Start(<-chan struct{}) error { return nil }

var _ configmap.Watcher = (*mockWatcher)(nil)

var sortStrings = cmpopts.SortSlices(func(x, y string) bool {
	return x < y
})
