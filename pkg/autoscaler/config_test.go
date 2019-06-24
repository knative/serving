/*
Copyright 2018 The Knative Authors.

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

package autoscaler

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"

	. "github.com/knative/pkg/configmap/testing"
)

func TestNewConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]string
		want    *Config
		wantErr bool
	}{{
		name: "minimum",
		input: map[string]string{
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		want: &Config{
			EnableScaleToZero:                  true,
			ContainerConcurrencyTargetFraction: 0.5,
			ContainerConcurrencyTargetDefault:  10.0,
			MaxScaleUpRate:                     1.0,
			StableWindow:                       5 * time.Minute,
			PanicWindow:                        10 * time.Second,
			ScaleToZeroGracePeriod:             30 * time.Second,
			TickInterval:                       2 * time.Second,
			PanicWindowPercentage:              10.0,
			PanicThresholdPercentage:           200.0,
		},
	}, {
		name: "concurrencty target percentage as percent",
		input: map[string]string{
			"enable-scale-to-zero":                    "true",
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "50",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		want: &Config{
			EnableScaleToZero:                  true,
			ContainerConcurrencyTargetFraction: 0.5,
			ContainerConcurrencyTargetDefault:  10.0,
			MaxScaleUpRate:                     1.0,
			StableWindow:                       5 * time.Minute,
			PanicWindow:                        10 * time.Second,
			ScaleToZeroGracePeriod:             30 * time.Second,
			TickInterval:                       2 * time.Second,
			PanicWindowPercentage:              10.0,
			PanicThresholdPercentage:           200.0,
		},
	}, {
		name: "with toggles on",
		input: map[string]string{
			"enable-scale-to-zero":                    "true",
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		want: &Config{
			EnableScaleToZero:                  true,
			ContainerConcurrencyTargetFraction: 0.5,
			ContainerConcurrencyTargetDefault:  10.0,
			MaxScaleUpRate:                     1.0,
			StableWindow:                       5 * time.Minute,
			PanicWindow:                        10 * time.Second,
			ScaleToZeroGracePeriod:             30 * time.Second,
			TickInterval:                       2 * time.Second,
			PanicWindowPercentage:              10.0,
			PanicThresholdPercentage:           200.0,
		},
	}, {
		name: "with toggles on strange casing",
		input: map[string]string{
			"enable-scale-to-zero":                    "TRUE",
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		want: &Config{
			EnableScaleToZero:                  true,
			ContainerConcurrencyTargetFraction: 0.5,
			ContainerConcurrencyTargetDefault:  10.0,
			MaxScaleUpRate:                     1.0,
			StableWindow:                       5 * time.Minute,
			PanicWindow:                        10 * time.Second,
			ScaleToZeroGracePeriod:             30 * time.Second,
			TickInterval:                       2 * time.Second,
			PanicWindowPercentage:              10.0,
			PanicThresholdPercentage:           200.0,
		},
	}, {
		name: "with toggles explicitly off",
		input: map[string]string{
			"enable-scale-to-zero":                    "false",
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		want: &Config{
			ContainerConcurrencyTargetFraction: 0.5,
			ContainerConcurrencyTargetDefault:  10.0,
			MaxScaleUpRate:                     1.0,
			StableWindow:                       5 * time.Minute,
			PanicWindow:                        10 * time.Second,
			ScaleToZeroGracePeriod:             30 * time.Second,
			TickInterval:                       2 * time.Second,
			PanicWindowPercentage:              10.0,
			PanicThresholdPercentage:           200.0,
		},
	}, {
		name: "with explicit grace period",
		input: map[string]string{
			"enable-scale-to-zero":                    "false",
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"scale-to-zero-grace-period":              "30s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		want: &Config{
			ContainerConcurrencyTargetFraction: 0.5,
			ContainerConcurrencyTargetDefault:  10.0,
			MaxScaleUpRate:                     1.0,
			StableWindow:                       5 * time.Minute,
			PanicWindow:                        10 * time.Second,
			ScaleToZeroGracePeriod:             30 * time.Second,
			TickInterval:                       2 * time.Second,
			PanicWindowPercentage:              10.0,
			PanicThresholdPercentage:           200.0,
		},
	}, {
		name: "malformed float",
		input: map[string]string{
			"max-scale-up-rate":                       "not a float",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		wantErr: true,
	}, {
		name: "malformed duration",
		input: map[string]string{
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "not a duration",
			"panic-window":                            "10s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		wantErr: true,
	}, {
		name: "invalid target %, too small",
		input: map[string]string{
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "-42",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "30s",
			"panic-window":                            "10s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		wantErr: true,
	}, {
		name: "invalid target %, too big",
		input: map[string]string{
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "142.4",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "30s",
			"panic-window":                            "10s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		wantErr: true,
	}, {
		name: "target capacity less than 1",
		input: map[string]string{
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "30.0",
			"container-concurrency-target-default":    "2",
			"stable-window":                           "30s",
			"panic-window":                            "10s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		wantErr: true,
	}, {
		name: "stable window too small",
		input: map[string]string{
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "1s",
			"panic-window":                            "10s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		wantErr: true,
	}, {
		name: "panic window too small",
		input: map[string]string{
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "12s",
			"panic-window":                            "500ms",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		wantErr: true,
	}, {
		name: "panic window too big",
		input: map[string]string{
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "12s",
			"panic-window":                            "13s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10",
			"panic-threshold-percentage":              "200",
		},
		wantErr: true,
	}, {
		name: "panic window percentage too small",
		input: map[string]string{
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "12s",
			"panic-window":                            "5s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "10", // 1.2s < BucketSize
			"panic-threshold-percentage":              "200",
		},
		wantErr: true,
	}, {
		name: "panic window percentage too big",
		input: map[string]string{
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "12s",
			"panic-window":                            "3s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "110",
			"panic-threshold-percentage":              "200",
		},
		wantErr: true,
	}, {
		name: "grace window too big",
		input: map[string]string{
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "12s",
			"panic-window":                            "3s",
			"scale-to-zero-grace-period":              "4s",
			"tick-interval":                           "2s",
			"panic-window-percentage":                 "50",
			"panic-threshold-percentage":              "200",
		},
		wantErr: true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NewConfigFromConfigMap(&corev1.ConfigMap{
				Data: test.input,
			})
			if (err != nil) != test.wantErr {
				t.Errorf("NewConfig() = %v, want %v", err, test.wantErr)
			}
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("NewConfig (-want, +got) = %v", diff)
			}
		})
	}
}

func TestOurConfig(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, ConfigName)
	if _, err := NewConfigFromConfigMap(cm); err != nil {
		t.Errorf("NewConfigFromConfigMap(actual) = %v", err)
	}
	if _, err := NewConfigFromConfigMap(example); err != nil {
		t.Errorf("NewConfigFromConfigMap(example) = %v", err)
	}
}
