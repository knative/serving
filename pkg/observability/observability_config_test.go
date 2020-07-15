/*
Copyright 2020 The Knative Authors.

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

package observation

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"knative.dev/pkg/metrics"

	. "knative.dev/pkg/configmap/testing"
	_ "knative.dev/pkg/system/testing"
)

func TestNewConfigMap(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, metrics.ConfigMapName())

	realCfg, err := metrics.NewObservabilityConfigFromConfigMap(cm)
	if err != nil {
		t.Fatal("NewObservabilityConfigFromConfigMap(actual) =", err)
	}
	if realCfg == nil {
		t.Fatal("NewObservabilityConfigFromConfigMap(actual) = nil")
	}

	exCfg, err := metrics.NewObservabilityConfigFromConfigMap(example)
	if err != nil {
		t.Fatal("NewObservabilityConfigFromConfigMap(example) =", err)
	}
	if exCfg == nil {
		t.Fatal("NewObservabilityConfigFromConfigMap(example) = nil")
	}
	// TODO(#8644): remove this.
	co := cmpopts.IgnoreFields(metrics.ObservabilityConfig{}, "RequestLogTemplate")
	if !cmp.Equal(realCfg, exCfg, co) {
		t.Errorf("actual != example: diff(-actual,+exCfg):\n%s", cmp.Diff(realCfg, exCfg, co))
	}
}
