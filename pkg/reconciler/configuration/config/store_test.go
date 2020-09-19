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
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/resource"
	logtesting "knative.dev/pkg/logging/testing"
	cfgmap "knative.dev/serving/pkg/apis/config"

	. "knative.dev/pkg/configmap/testing"
)

var ignoreStuff = cmp.Options{
	cmpopts.IgnoreUnexported(resource.Quantity{}),
}

func TestStoreLoadWithContext(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	defaultsConfig := ConfigMapFromTestFile(t, cfgmap.DefaultsConfigName)
	featuresConfig := ConfigMapFromTestFile(t, cfgmap.FeaturesConfigName)

	store.OnConfigChanged(defaultsConfig)
	store.OnConfigChanged(featuresConfig)

	config := FromContextOrDefaults(store.ToContext(context.Background()))

	t.Run("defaults", func(t *testing.T) {
		expected, _ := cfgmap.NewDefaultsConfigFromConfigMap(defaultsConfig)
		if got, want := config.Defaults, expected; !cmp.Equal(got, want, ignoreStuff...) {
			t.Errorf("Unexpected defaults config = %v, want: %v, diff (-want, +got):\n%s", got, want, cmp.Diff(want, got, ignoreStuff...))
		}
	})

	t.Run("features", func(t *testing.T) {
		expected, _ := cfgmap.NewFeaturesConfigFromConfigMap(featuresConfig)
		if got, want := config.Features, expected; !cmp.Equal(got, want, ignoreStuff...) {
			t.Errorf("Unexpected features config = %v, want: %v, diff (-want, +got):\n%s", got, want, cmp.Diff(want, got, ignoreStuff...))
		}
	})
}

func TestStoreLoadWithContextOrDefaults(t *testing.T) {
	defaultsConfig := ConfigMapFromTestFile(t, cfgmap.DefaultsConfigName)
	featuresConfig := ConfigMapFromTestFile(t, cfgmap.FeaturesConfigName)

	config := FromContextOrDefaults(context.Background())

	t.Run("defaults", func(t *testing.T) {
		expected, _ := cfgmap.NewDefaultsConfigFromConfigMap(defaultsConfig)
		if got, want := config.Defaults, expected; !cmp.Equal(got, want, ignoreStuff...) {
			t.Errorf("Unexpected defaults config = %v, want: %v, diff (-want, +got):\n%s", got, want, cmp.Diff(want, got, ignoreStuff...))
		}
	})

	t.Run("features", func(t *testing.T) {
		expected, _ := cfgmap.NewFeaturesConfigFromConfigMap(featuresConfig)
		if got, want := config.Features, expected; !cmp.Equal(got, want, ignoreStuff...) {
			t.Errorf("Unexpected features config = %v, want: %v, diff (-want, +got):\n%s", got, want, cmp.Diff(want, got, ignoreStuff...))
		}
	})
}

func TestStoreImmutableConfig(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	store.OnConfigChanged(ConfigMapFromTestFile(t, cfgmap.DefaultsConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, cfgmap.FeaturesConfigName))

	config := store.Load()

	config.Defaults.RevisionTimeoutSeconds = 1234
	config.Features.MultiContainer = cfgmap.Disabled

	newConfig := store.Load()

	if newConfig.Defaults.RevisionTimeoutSeconds == 1234 {
		t.Error("Defaults config is not immutable")
	}

	if newConfig.Features.MultiContainer == cfgmap.Disabled {
		t.Error("Features config is not immutable")
	}
}
