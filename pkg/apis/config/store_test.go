/*
Copyright 2019 The Knative Authors.

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
	"k8s.io/apimachinery/pkg/api/resource"
	logtesting "knative.dev/pkg/logging/testing"

	. "knative.dev/pkg/configmap/testing"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
)

var ignoreStuff = cmp.Options{
	cmpopts.IgnoreUnexported(resource.Quantity{}),
}

func TestStoreLoadWithContext(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	defaultsConfig := ConfigMapFromTestFile(t, DefaultsConfigName)
	featuresConfig := ConfigMapFromTestFile(t, FeaturesConfigName)
	autoscalerConfig := ConfigMapFromTestFile(t, autoscalerconfig.ConfigName)

	store.OnConfigChanged(defaultsConfig)
	store.OnConfigChanged(featuresConfig)
	store.OnConfigChanged(autoscalerConfig)

	config := FromContextOrDefaults(store.ToContext(context.Background()))

	t.Run("defaults", func(t *testing.T) {
		expected, _ := NewDefaultsConfigFromConfigMap(defaultsConfig)
		if diff := cmp.Diff(expected, config.Defaults, ignoreStuff...); diff != "" {
			t.Errorf("Unexpected defaults config (-want, +got):\n%v", diff)
		}
	})

	t.Run("features", func(t *testing.T) {
		expected, _ := NewFeaturesConfigFromConfigMap(featuresConfig)
		if diff := cmp.Diff(expected, config.Features, ignoreStuff...); diff != "" {
			t.Errorf("Unexpected features config (-want, +got):\n%v", diff)
		}
	})

	t.Run("autoscaler", func(t *testing.T) {
		expected, _ := autoscalerconfig.NewConfigFromConfigMap(autoscalerConfig)
		if diff := cmp.Diff(expected, config.Autoscaler, ignoreStuff...); diff != "" {
			t.Errorf("Unexpected autoscaler config (-want, +got):\n%v", diff)
		}
	})
}

func TestStoreLoadWithContextOrDefaults(t *testing.T) {
	defaultsConfig := ConfigMapFromTestFile(t, DefaultsConfigName)
	featuresConfig := ConfigMapFromTestFile(t, FeaturesConfigName)
	autoscalerConfig := ConfigMapFromTestFile(t, autoscalerconfig.ConfigName)
	config := FromContextOrDefaults(context.Background())

	t.Run("defaults", func(t *testing.T) {
		expected, _ := NewDefaultsConfigFromConfigMap(defaultsConfig)
		if diff := cmp.Diff(expected, config.Defaults, ignoreStuff...); diff != "" {
			t.Errorf("Unexpected defaults config (-want, +got):\n%v", diff)
		}
	})

	t.Run("features", func(t *testing.T) {
		expected, _ := NewFeaturesConfigFromConfigMap(featuresConfig)
		if diff := cmp.Diff(expected, config.Features, ignoreStuff...); diff != "" {
			t.Errorf("Unexpected features config (-want, +got):\n%v", diff)
		}
	})

	t.Run("autoscaler", func(t *testing.T) {
		expected, _ := autoscalerconfig.NewConfigFromConfigMap(autoscalerConfig)
		if diff := cmp.Diff(expected, config.Autoscaler, ignoreStuff...); diff != "" {
			t.Errorf("Unexpected autoscaler config (-want, +got):\n%v", diff)
		}
	})
}

func TestStoreImmutableConfig(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	store.OnConfigChanged(ConfigMapFromTestFile(t, DefaultsConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, FeaturesConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, autoscalerconfig.ConfigName))

	config := store.Load()

	config.Defaults.RevisionTimeoutSeconds = 1234
	config.Features.MultiContainer = Disabled
	config.Autoscaler.TargetBurstCapacity = 99

	newConfig := store.Load()

	if newConfig.Defaults.RevisionTimeoutSeconds == 1234 {
		t.Error("Defaults config is not immutable")
	}

	if newConfig.Features.MultiContainer == Disabled {
		t.Error("Features config is not immutable")
	}

	if newConfig.Autoscaler.TargetBurstCapacity == 99 {
		t.Error("Autoscaler config is not immutable")
	}
}
