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
	logtesting "github.com/knative/pkg/logging/testing"

	. "github.com/knative/pkg/configmap/testing"
	"github.com/knative/serving/pkg/autoscaler"
)

func TestStoreLoadWithContext(t *testing.T) {
	defer logtesting.ClearAll()
	store := NewStore(logtesting.TestLogger(t))

	autoscalerConfig := ConfigMapFromTestFile(t, autoscaler.ConfigName)
	store.OnConfigChanged(autoscalerConfig)
	config := FromContext(store.ToContext(context.Background()))

	want, _ := autoscaler.NewConfigFromConfigMap(autoscalerConfig)
	if diff := cmp.Diff(want, config.Autoscaler); diff != "" {
		t.Errorf("Unexpected TLS mode (-want, +got): %s", diff)
	}
}

func TestStoreImmutableConfig(t *testing.T) {
	defer logtesting.ClearAll()
	store := NewStore(logtesting.TestLogger(t))

	store.OnConfigChanged(ConfigMapFromTestFile(t, autoscaler.ConfigName))

	config := store.Load()
	config.Autoscaler.MaxScaleUpRate = 100.0
	newConfig := store.Load()

	if newConfig.Autoscaler.MaxScaleUpRate == 100.0 {
		t.Error("Autoscaler config is not immuable")
	}
}
