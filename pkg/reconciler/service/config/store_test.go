/*
Copyright 2020 The Knative Authors

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

	"github.com/google/go-cmp/cmp"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/network"

	. "knative.dev/pkg/configmap/testing"
)

func TestStoreLoadWithContext(t *testing.T) {
	ctx := logtesting.TestContextWithLogger(t)
	store := NewStore(ctx)

	networkConfig := ConfigMapFromTestFile(t, network.ConfigName)

	store.OnConfigChanged(networkConfig)

	config := FromContext(store.ToContext(context.Background()))

	t.Run("network", func(t *testing.T) {
		expected, _ := network.NewConfigFromConfigMap(networkConfig)
		if diff := cmp.Diff(expected, config.Network); diff != "" {
			t.Errorf("Unexpected controller config (-want, +got): %v", diff)
		}
	})

}

func TestStoreImmutableConfig(t *testing.T) {
	store := NewStore(logtesting.TestContextWithLogger(t))
	store.OnConfigChanged(ConfigMapFromTestFile(t, network.ConfigName))

	config := store.Load()

	config.Network.IstioOutboundIPRanges = "mutated"

	newConfig := store.Load()

	if newConfig.Network.IstioOutboundIPRanges == "mutated" {
		t.Error("Network config is not immutable")
	}
}
