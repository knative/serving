/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

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
	logtesting "github.com/knative/pkg/logging/testing"
	"github.com/knative/serving/pkg/gc"
	"github.com/knative/serving/pkg/network"

	. "github.com/knative/pkg/configmap/testing"
)

func TestStoreLoadWithContext(t *testing.T) {
	defer logtesting.ClearAll()
	store := NewStore(logtesting.TestLogger(t), 10*time.Hour)

	domainConfig := ConfigMapFromTestFile(t, DomainConfigName)
	gcConfig := ConfigMapFromTestFile(t, gc.ConfigName)
	networkConfig := ConfigMapFromTestFile(t, network.ConfigName)

	store.OnConfigChanged(domainConfig)
	store.OnConfigChanged(gcConfig)
	store.OnConfigChanged(networkConfig)

	config := FromContext(store.ToContext(context.Background()))

	t.Run("domain", func(t *testing.T) {
		expected, _ := NewDomainFromConfigMap(domainConfig)
		if diff := cmp.Diff(expected, config.Domain); diff != "" {
			t.Errorf("Unexpected controller config (-want, +got): %v", diff)
		}
	})

	t.Run("gc", func(t *testing.T) {
		expected, err := gc.NewConfigFromConfigMapFunc(logtesting.TestLogger(t), 10*time.Hour)(gcConfig)
		if err != nil {
			t.Errorf("Parsing configmap: %v", err)
		}
		if diff := cmp.Diff(expected, config.GC); diff != "" {
			t.Errorf("Unexpected controller config (-want, +got): %v", diff)
		}
	})

	t.Run("gc invalid timeout", func(t *testing.T) {
		gcConfig.Data["stale-revision-timeout"] = "1h"
		expected, err := gc.NewConfigFromConfigMapFunc(logtesting.TestLogger(t), 10*time.Hour)(gcConfig)

		if err != nil {
			t.Errorf("Got error parsing gc config with invalid timeout: %v", err)
		}

		if expected.StaleRevisionTimeout != 15*time.Hour {
			t.Errorf("Expected revision timeout of %v, got %v", 15*time.Hour, expected.StaleRevisionTimeout)
		}
	})
}

func TestStoreImmutableConfig(t *testing.T) {
	defer logtesting.ClearAll()
	store := NewStore(logtesting.TestLogger(t), 10*time.Hour)
	store.OnConfigChanged(ConfigMapFromTestFile(t, DomainConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, network.ConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, gc.ConfigName))

	config := store.Load()

	config.Domain.Domains = map[string]*LabelSelector{
		"mutated": nil,
	}

	newConfig := store.Load()

	if _, ok := newConfig.Domain.Domains["mutated"]; ok {
		t.Error("Domain config is not immutable")
	}
}
