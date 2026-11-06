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

package config

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	netcfg "knative.dev/networking/pkg/config"
	logtesting "knative.dev/pkg/logging/testing"
	apisconfig "knative.dev/serving/pkg/apis/config"

	. "knative.dev/pkg/configmap/testing"
)

func TestStoreLoadWithContext(t *testing.T) {
	ctx := logtesting.TestContextWithLogger(t)
	store := NewStore(ctx)

	networkConfig := ConfigMapFromTestFile(t, netcfg.ConfigMapName)
	featuresConfig := ConfigMapFromTestFile(t, apisconfig.FeaturesConfigName)
	store.OnConfigChanged(networkConfig)
	store.OnConfigChanged(featuresConfig)

	config := FromContext(store.ToContext(context.Background()))

	t.Run("network", func(t *testing.T) {
		expected, _ := netcfg.NewConfigFromConfigMap(networkConfig)
		if diff := cmp.Diff(expected, config.Network); diff != "" {
			t.Errorf("Unexpected network config (-want, +got):\n%v", diff)
		}
	})

	t.Run("features", func(t *testing.T) {
		expected, _ := apisconfig.NewFeaturesConfigFromConfigMap(featuresConfig)
		if diff := cmp.Diff(expected, config.Features); diff != "" {
			t.Errorf("Unexpected features config (-want, +got):\n%v", diff)
		}
	})
}

func TestStoreLoadWithContextOrDefaults(t *testing.T) {
	store := NewStore(logtesting.TestContextWithLogger(t))
	store.OnConfigChanged(ConfigMapFromTestFile(t, netcfg.ConfigMapName))

	config := FromContextOrDefaults(store.ToContext(context.Background()))

	t.Run("features", func(t *testing.T) {
		expected, _ := apisconfig.NewFeaturesConfigFromMap(map[string]string{})
		if diff := cmp.Diff(expected, config.Features); diff != "" {
			t.Errorf("Unexpected features config (-want, +got):\n%v", diff)
		}
	})
}

func TestFromContextOrDefaultsWithoutConfig(t *testing.T) {
	config := FromContextOrDefaults(context.Background())

	expected, _ := apisconfig.NewFeaturesConfigFromMap(map[string]string{})
	if diff := cmp.Diff(expected, config.Features); diff != "" {
		t.Errorf("Unexpected features config (-want, +got):\n%v", diff)
	}
}
