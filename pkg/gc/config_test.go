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
	x
	actual, example := ConfigMapsFromTestFile(t, "config-gc")
	for _, tt := range []struct {
		name string
		fail bool
		want *Config
		data map[string]string
	}{{
		name: "actual config",
		fail: false,
		want: defaultConfig(),
		data: actual.Data,
	}, {
		name: "example config",
		fail: false,
		want: defaultConfig(),
		data: example.Data,
	}, {
		name: "with value overrides",
		want: &Config{
			StaleRevisionCreateDelay:        15 * time.Hour,
			StaleRevisionTimeout:            14 * time.Hour,
			StaleRevisionMinimumGenerations: 10,
			StaleRevisionLastpinnedDebounce: 2*time.Hour + 30*time.Minute + 44*time.Second,
			RetainSinceCreateTime:           17 * time.Hour,
			RetainSinceLastActiveTime:       16 * time.Hour,
			MinNonActiveRevisions:           5,
			MaxNonActiveRevisions:           500,
		},
		data: map[string]string{
			"stale-revision-create-delay":        "15h",
			"stale-revision-timeout":             "14h",
			"stale-revision-minimum-generations": "10",
			"stale-revision-lastpinned-debounce": "2h30m44s",
			"retain-since-create-time":           "17h",
			"retain-since-last-active-time":      "16h",
			"min-non-active-revisions":           "5",
			"max-non-active-revisions":           "500",
		},
	}, {
		name: "invalid duration",
		fail: true,
		data: map[string]string{
			"stale-revision-create-delay": "invalid",
		},
	}, {
		name: "invalid negative minimum generation",
		fail: true,
		data: map[string]string{
			"stale-revision-minimum-generations": "-1",
		},
	}, {
		name: "invalid minimum generation",
		fail: true,
		data: map[string]string{
			"stale-revision-minimum-generations": "invalid",
		},
	}, {
		name: "Invalid negative min stale",
		fail: true,
		data: map[string]string{
			"min-non-active-revisions": "-1",
		},
	}, {
		name: "Invalid negative maximum generation",
		fail: true,
		data: map[string]string{
			"max-non-active-revisions": "-2",
		},
	}, {
		name: "invalid max less than min",
		fail: true,
		data: map[string]string{
			"min-non-active-revisions": "20",
			"max-non-active-revisions": "10",
		},
	}, {
		name: "unparsable create duration",
		fail: true,
		data: map[string]string{
			"retain-since-create-time": "invalid",
		},
	}, {
		name: "negative create duration",
		fail: true,
		data: map[string]string{
			"retain-since-create-time": "-1h",
		},
	}, {
		name: "negative last-active duration",
		fail: true,
		data: map[string]string{
			"retain-since-last-active-time": "-1h",
		},
	}, {
		name: "create delay disabled",
		want: func() *Config {
			d := defaultConfig()
			d.RetainSinceCreateTime = time.Duration(Disabled)
			return d
		}(),
		data: map[string]string{
			"retain-since-create-time": disabled,
		},
	}, {
		name: "last-active disabled",
		want: func() *Config {
			d := defaultConfig()
			d.RetainSinceLastActiveTime = time.Duration(Disabled)
			return d
		}(),
		data: map[string]string{
			"retain-since-last-active-time": disabled,
		},
	}, {
		name: "max-non-active unparsable",
		fail: true,
		data: map[string]string{
			"max-non-active-revisions": "invalid",
		},
	}, {
		name: "max-non-active disabled",
		want: func() *Config {
			d := defaultConfig()
			d.MaxNonActiveRevisions = Disabled
			return d
		}(),
		data: map[string]string{
			"max-non-active-revisions": disabled,
		},
	}, {
		name: "below minimum timeout",
		fail: false,
		want: &Config{
			StaleRevisionCreateDelay:        15 * time.Hour,
			StaleRevisionTimeout:            15 * time.Hour,
			StaleRevisionMinimumGenerations: 20,
			StaleRevisionLastpinnedDebounce: 5 * time.Hour,
			RetainSinceCreateTime:           48 * time.Hour,
			RetainSinceLastActiveTime:       15 * time.Hour,
			MinNonActiveRevisions:           20,
			MaxNonActiveRevisions:           1000,
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
