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

package gc

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/knative/serving/pkg/reconciler/testing"
)

func TestOurConfig(t *testing.T) {
	for _, tt := range []struct {
		name string
		fail bool
		want Config
		data string
	}{
		{
			"Standard config",
			false,
			Config{
				StaleRevisionCreateDelay:        24 * time.Hour,
				StaleRevisionTimeout:            15 * time.Hour,
				StaleRevisionMinimumGenerations: 1,
				StaleRevisionLastpinnedDebounce: 5 * time.Hour,
			},
			"config-gc",
		}, {
			"Defaulted config",
			false,
			Config{
				StaleRevisionCreateDelay:        24 * time.Hour,
				StaleRevisionTimeout:            15 * time.Hour,
				StaleRevisionMinimumGenerations: 1,
				StaleRevisionLastpinnedDebounce: 5 * time.Hour,
			},
			"config-gc-defaults",
		}, {
			"Invalid duration",
			true,
			Config{},
			"config-fail-duration",
		}, {
			"Invalid duration",
			true,
			Config{},
			"config-fail-minimum-generations",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cm := ConfigMapFromTestFile(t, tt.data)
			testConfig, err := NewConfigFromConfigMap(cm)

			if tt.fail != (err != nil) {
				t.Errorf("Unexpected error value: %v", err)
			}

			if tt.fail {
				return
			}

			if diff := cmp.Diff(tt.want, *testConfig); diff != "" {
				t.Errorf("Unexpected controller config (-want, +got): %v", diff)
			}
		})
	}
}
