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

package leaderelection

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"

	. "knative.dev/pkg/configmap/testing"
	kle "knative.dev/pkg/leaderelection"
)

func okConfig() *kle.Config {
	return &kle.Config{
		Buckets:       5,
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
	}
}

func okData() map[string]string {
	return map[string]string{
		"buckets": "5",
		// values in this data come from the defaults suggested in the
		// code:
		// https://github.com/kubernetes/client-go/blob/kubernetes-1.16.0/tools/leaderelection/leaderelection.go
		"leaseDuration": "15s",
		"renewDeadline": "10s",
		"retryPeriod":   "2s",
	}
}

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		name     string
		data     map[string]string
		expected *kle.Config
		err      string
	}{{
		name:     "OK",
		data:     okData(),
		expected: okConfig(),
	}, {
		name: "bad time",
		data: func() map[string]string {
			data := okData()
			data["renewDeadline"] = "not a duration"
			return data
		}(),
		err: `failed to parse "renewDeadline": time: invalid duration`,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actualConfig, actualErr := ValidateConfig(&corev1.ConfigMap{Data: tc.data})

			if actualErr != nil {
				// Different versions of Go quote input differently, so check prefix.
				if got, want := actualErr.Error(), tc.err; !strings.HasPrefix(got, want) {
					t.Fatalf("Err = '%s', want: '%s'", got, want)
				}
			} else if tc.err != "" {
				t.Fatal("Expected an error, got none")
			}
			if got, want := actualConfig, tc.expected; !cmp.Equal(got, want) {
				t.Errorf("Config = %v, want: %v, diff(-want,+got):\n%s", got, want, cmp.Diff(want, got))
			}
		})
	}
}

func TestServingConfig(t *testing.T) {
	actual, example := ConfigMapsFromTestFile(t, kle.ConfigMapName())
	for _, test := range []struct {
		name string
		data *corev1.ConfigMap
		want *kle.Config
	}{{
		name: "Default config",
		want: &kle.Config{
			Buckets:       1,
			LeaseDuration: 15 * time.Second,
			RenewDeadline: 10 * time.Second,
			RetryPeriod:   2 * time.Second,
		},
		data: actual,
	}, {
		name: "Example config",
		want: &kle.Config{
			Buckets:       1,
			LeaseDuration: 15 * time.Second,
			RenewDeadline: 10 * time.Second,
			RetryPeriod:   2 * time.Second,
		},
		data: example,
	}} {
		t.Run(test.name, func(t *testing.T) {
			cm, err := ValidateConfig(test.data)
			if err != nil {
				t.Fatal("Error parsing config =", err)
			}
			if got, want := cm, test.want; !cmp.Equal(got, want) {
				t.Errorf("Config mismatch: (-want,+got):\n%s", cmp.Diff(want, got))
			}
		})
	}
}
