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
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	. "knative.dev/pkg/configmap/testing"
	kle "knative.dev/pkg/leaderelection"
)

func okConfig() *kle.Config {
	return &kle.Config{
		ResourceLock:      "leases",
		LeaseDuration:     15 * time.Second,
		RenewDeadline:     10 * time.Second,
		RetryPeriod:       2 * time.Second,
		EnabledComponents: sets.NewString("controller"),
	}
}

func okData() map[string]string {
	return map[string]string{
		"resourceLock": "leases",
		// values in this data come from the defaults suggested in the
		// code:
		// https://github.com/kubernetes/client-go/blob/kubernetes-1.16.0/tools/leaderelection/leaderelection.go
		"leaseDuration":     "15s",
		"renewDeadline":     "10s",
		"retryPeriod":       "2s",
		"enabledComponents": "controller",
	}
}

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		name     string
		data     map[string]string
		expected *kle.Config
		err      error
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
		err: errors.New(`renewDeadline: invalid duration: "not a duration"`),
	}, {
		name: "invalid component",
		data: func() map[string]string {
			data := okData()
			data["enabledComponents"] = "controller,frobulator"
			return data
		}(),
		err: errors.New(`invalid enabledComponent "frobulator": valid values are ["certcontroller" "controller" "hpaautoscaler" "istiocontroller" "nscontroller"]`),
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actualConfig, actualErr := ValidateConfig(&corev1.ConfigMap{Data: tc.data})
			if !reflect.DeepEqual(tc.err, actualErr) {
				t.Fatalf("%v: expected error %v, got %v", tc.name, tc.err, actualErr)
			}

			if got, want := actualConfig, tc.expected; !cmp.Equal(got, want) {
				t.Errorf("Config = %v, want: %v, diff(-want,+got):\n%s", got, want, cmp.Diff(want, got))
			}
		})
	}
}
func TestServingConfig(t *testing.T) {
	actual, example := ConfigMapsFromTestFile(t, "config-leader-election",
		"resourceLock", "leaseDuration", "renewDeadline", "retryPeriod")
	for _, test := range []struct {
		name string
		data *corev1.ConfigMap
		want *kle.Config
	}{{
		name: "Default config",
		want: &kle.Config{
			ResourceLock:  "leases",
			LeaseDuration: 15 * time.Second,
			RenewDeadline: 10 * time.Second,
			RetryPeriod:   2 * time.Second,
		},
		data: actual,
	}, {
		name: "Example config",
		want: &kle.Config{
			ResourceLock:      "leases",
			LeaseDuration:     15 * time.Second,
			RenewDeadline:     10 * time.Second,
			RetryPeriod:       2 * time.Second,
			EnabledComponents: validComponents,
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
