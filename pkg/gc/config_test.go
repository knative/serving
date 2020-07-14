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
	corev1 "k8s.io/api/core/v1"

	. "knative.dev/pkg/configmap/testing"
	logtesting "knative.dev/pkg/logging/testing"
)

func TestOurConfig(t *testing.T) {
	actual, example := ConfigMapsFromTestFile(t, "config-gc")
	for _, tt := range []struct {
		name string
		fail bool
		want *Config
		data map[string]string
	}{{
		name: "Actual config",
		fail: false,
		want: defaultConfig(),
		data: actual.Data,
	}, {
		name: "Example config",
		fail: false,
		want: defaultConfig(),
		data: example.Data,
	}, {
		name: "With value overrides",
		want: &Config{
			StaleRevisionCreateDelay:        15 * time.Hour,
			StaleRevisionTimeout:            14 * time.Hour,
			StaleRevisionMinimumGenerations: 10,
			StaleRevisionLastpinnedDebounce: 2*time.Hour + 30*time.Minute + 44*time.Second,
			RetainSinceCreateTime:           17 * time.Hour,
			RetainSinceLastActiveTime:       16 * time.Hour,
			MinStaleRevisions:               5,
			MaxStaleRevisions:               500,
		},
		data: map[string]string{
			"stale-revision-create-delay":        "15h",
			"stale-revision-timeout":             "14h",
			"stale-revision-minimum-generations": "10",
			"stale-revision-lastpinned-debounce": "2h30m44s",
			"retain-since-create-time":           "17h",
			"retain-since-last-active-time":      "16h",
			"min-stale-revisions":                "5",
			"max-stale-revisions":                "500",
		},
	}, {
		name: "Invalid duration",
		fail: true,
		want: nil,
		data: map[string]string{
			"stale-revision-create-delay": "invalid",
		},
	}, {
		name: "Invalid negative minimum generation",
		fail: true,
		want: nil,
		data: map[string]string{
			"stale-revision-minimum-generations": "-1",
		},
	}, {
		name: "Invalid negative minimum generation v2",
		fail: true,
		want: nil,
		data: map[string]string{
			"min-stale-revisions": "-1",
		},
	}, {
		name: "Invalid negative maximum generation",
		fail: true,
		want: nil,
		data: map[string]string{
			"max-stale-revisions": "-2",
		},
	}, {
		name: "Invalid max less than min",
		fail: true,
		want: nil,
		data: map[string]string{
			"min-stale-revisions": "20",
			"max-stale-revisions": "10",
		},
	}, {
		name: "Invalid minimum generation",
		fail: true,
		want: nil,
		data: map[string]string{
			"stale-revision-minimum-generations": "invalid",
		},
	}, {
		name: "Below minimum timeout",
		fail: false,
		want: &Config{
			StaleRevisionCreateDelay:        15 * time.Hour,
			StaleRevisionTimeout:            15 * time.Hour,
			StaleRevisionMinimumGenerations: 20,
			StaleRevisionLastpinnedDebounce: 5 * time.Hour,
			RetainSinceCreateTime:           48 * time.Hour,
			RetainSinceLastActiveTime:       15 * time.Hour,
			MinStaleRevisions:               20,
			MaxStaleRevisions:               -1,
		},
		data: map[string]string{
			"stale-revision-create-delay": "15h",
			"stale-revision-timeout":      "1h",
		},
	}} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewConfigFromConfigMapFunc(logtesting.TestContextWithLogger(t))(
				&corev1.ConfigMap{Data: tt.data})
			if tt.fail != (err != nil) {
				t.Fatal("Unexpected error value:", err)
			}

			if !cmp.Equal(tt.want, got) {
				t.Errorf("GC config (-want, +got): %s", cmp.Diff(tt.want, got))
			}
		})
	}
}
