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
	"time"

	"github.com/google/go-cmp/cmp"
	logtesting "knative.dev/pkg/logging/testing"

	. "knative.dev/pkg/configmap/testing"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/deployment"
)

func TestStoreLoadWithContext(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	autoscalerConfig := ConfigMapFromTestFile(t, autoscalerconfig.ConfigName)
	depConfig := ConfigMapFromTestFile(t, deployment.ConfigName, deployment.QueueSidecarImageKey)
	store.OnConfigChanged(autoscalerConfig)
	store.OnConfigChanged(depConfig)
	config := FromContext(store.ToContext(context.Background()))

	wantAS, _ := autoscalerconfig.NewConfigFromConfigMap(autoscalerConfig)
	if !cmp.Equal(wantAS, config.Autoscaler) {
		t.Error("Autoscaler ConfigMap mismatch (-want, +got):", cmp.Diff(wantAS, config.Autoscaler))
	}
	wantD, _ := deployment.NewConfigFromConfigMap(depConfig)
	if !cmp.Equal(wantD, config.Deployment) {
		t.Error("Deployment ConfigMap mismatch (-want, +got):", cmp.Diff(wantD, config.Deployment))
	}
}

func TestStoreImmutableConfig(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	store.OnConfigChanged(ConfigMapFromTestFile(t, autoscalerconfig.ConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, deployment.ConfigName,
		deployment.QueueSidecarImageKey))

	config := store.Load()
	config.Autoscaler.MaxScaleUpRate = 100.0
	config.Deployment.ProgressDeadline = 3 * time.Minute
	newConfig := store.Load()

	if newConfig.Autoscaler.MaxScaleUpRate == 100.0 {
		t.Error("Autoscaler config is not immuable")
	}
	if newConfig.Deployment.ProgressDeadline == 3*time.Minute {
		t.Error("Autoscaler config is not immuable")
	}
}
