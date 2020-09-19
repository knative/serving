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
	"time"

	"github.com/google/go-cmp/cmp"
	network "knative.dev/networking/pkg"
	logtesting "knative.dev/pkg/logging/testing"
	cfgmap "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/gc"

	. "knative.dev/pkg/configmap/testing"
)

func TestStoreLoadWithContext(t *testing.T) {
	ctx := logtesting.TestContextWithLogger(t)
	store := NewStore(ctx)

	domainConfig := ConfigMapFromTestFile(t, DomainConfigName)
	gcConfig := ConfigMapFromTestFile(t, gc.ConfigName)
	networkConfig := ConfigMapFromTestFile(t, network.ConfigName)
	featureConfig := ConfigMapFromTestFile(t, cfgmap.FeaturesConfigName)

	store.OnConfigChanged(domainConfig)
	store.OnConfigChanged(gcConfig)
	store.OnConfigChanged(networkConfig)
	store.OnConfigChanged(featureConfig)

	config := FromContext(store.ToContext(context.Background()))

	t.Run("domain", func(t *testing.T) {
		expected, _ := NewDomainFromConfigMap(domainConfig)
		if diff := cmp.Diff(expected, config.Domain); diff != "" {
			t.Errorf("Unexpected controller config (-want, +got): %v", diff)
		}
	})

	t.Run("gc", func(t *testing.T) {
		expected, err := gc.NewConfigFromConfigMapFunc(ctx)(gcConfig)
		if err != nil {
			t.Errorf("Parsing configmap: %v", err)
		}
		if diff := cmp.Diff(expected, config.GC); diff != "" {
			t.Errorf("Unexpected controller config (-want, +got): %v", diff)
		}
	})

	t.Run("gc invalid timeout", func(t *testing.T) {
		gcConfig.Data["stale-revision-timeout"] = "1h"
		expected, err := gc.NewConfigFromConfigMapFunc(ctx)(gcConfig)

		if err != nil {
			t.Errorf("Got error parsing gc config with invalid timeout: %v", err)
		}

		if expected.StaleRevisionTimeout != 15*time.Hour {
			t.Errorf("Expected revision timeout of %v, got %v", 15*time.Hour, expected.StaleRevisionTimeout)
		}
	})
}

func TestStoreLoadWithContextOrDefaults(t *testing.T) {
	store := NewStore(logtesting.TestContextWithLogger(t))
	store.OnConfigChanged(ConfigMapFromTestFile(t, DomainConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, network.ConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, gc.ConfigName))

	config := FromContextOrDefaults(store.ToContext(context.Background()))

	t.Run("domain", func(t *testing.T) {
		expected, _ := cfgmap.NewFeaturesConfigFromMap(map[string]string{})
		if diff := cmp.Diff(expected, config.Features); diff != "" {
			t.Errorf("Unexpected controller config (-want, +got): %v", diff)
		}
	})
}

func TestStoreImmutableConfig(t *testing.T) {
	store := NewStore(logtesting.TestContextWithLogger(t))
	store.OnConfigChanged(ConfigMapFromTestFile(t, DomainConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, network.ConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, gc.ConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, cfgmap.FeaturesConfigName))

	config := store.Load()

	config.Domain.Domains = map[string]*LabelSelector{
		"mutated": nil,
	}

	newConfig := store.Load()

	if _, ok := newConfig.Domain.Domains["mutated"]; ok {
		t.Error("Domain config is not immutable")
	}
}
