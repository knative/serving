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
	"fmt"
	"io/ioutil"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

func TestTargetConcurrency(t *testing.T) {
	c := &Config{
		ContainerConcurrencyTargetPercentage: 0.5,
		ContainerConcurrencyTargetDefault:    10.0,
	}

	tests := []struct {
		name                 string
		containerConcurrency int
		want                 float64
	}{{
		name:                 "default",
		containerConcurrency: 0,
		want:                 10.0,
	}, {
		name:                 "single",
		containerConcurrency: 1,
		want:                 0.5,
	}, {
		name:                 "multi",
		containerConcurrency: 10,
		want:                 5.0,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := c.TargetConcurrency(v1alpha1.RevisionContainerConcurrencyType(test.containerConcurrency))
			if got != test.want {
				t.Errorf("TargetConcurrency() = %v, want %v", got, test.want)
			}
		})
	}
}

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
			"scale-to-zero-threshold":                 "10m",
			"tick-interval":                           "2s",
		},
		want: &Config{
			ContainerConcurrencyTargetPercentage: 0.5,
			ContainerConcurrencyTargetDefault:    10.0,
			MaxScaleUpRate:                       1.0,
			StableWindow:                         5 * time.Minute,
			PanicWindow:                          10 * time.Second,
			ScaleToZeroThreshold:                 10 * time.Minute,
			ScaleToZeroGracePeriod:               2 * time.Minute,
			ScaleToZeroIdlePeriod:                8 * time.Minute,
			TickInterval:                         2 * time.Second,
		},
	}, {
		name: "with toggles on",
		input: map[string]string{
			"enable-scale-to-zero":                    "true",
			"enable-vertical-pod-autoscaling":         "true",
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"scale-to-zero-threshold":                 "10m",
			"tick-interval":                           "2s",
		},
		want: &Config{
			EnableScaleToZero:                    true,
			EnableVPA:                            true,
			ContainerConcurrencyTargetPercentage: 0.5,
			ContainerConcurrencyTargetDefault:    10.0,
			MaxScaleUpRate:                       1.0,
			StableWindow:                         5 * time.Minute,
			PanicWindow:                          10 * time.Second,
			ScaleToZeroThreshold:                 10 * time.Minute,
			ScaleToZeroGracePeriod:               2 * time.Minute,
			ScaleToZeroIdlePeriod:                8 * time.Minute,
			TickInterval:                         2 * time.Second,
		},
	}, {
		name: "with toggles on strange casing",
		input: map[string]string{
			"enable-scale-to-zero":                    "TRUE",
			"enable-vertical-pod-autoscaling":         "True",
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"scale-to-zero-threshold":                 "10m",
			"tick-interval":                           "2s",
		},
		want: &Config{
			EnableScaleToZero:                    true,
			EnableVPA:                            true,
			ContainerConcurrencyTargetPercentage: 0.5,
			ContainerConcurrencyTargetDefault:    10.0,
			MaxScaleUpRate:                       1.0,
			StableWindow:                         5 * time.Minute,
			PanicWindow:                          10 * time.Second,
			ScaleToZeroThreshold:                 10 * time.Minute,
			ScaleToZeroGracePeriod:               2 * time.Minute,
			ScaleToZeroIdlePeriod:                8 * time.Minute,
			TickInterval:                         2 * time.Second,
		},
	}, {
		name: "with toggles explicitly off",
		input: map[string]string{
			"enable-scale-to-zero":                    "false",
			"enable-vertical-pod-autoscaling":         "False",
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"scale-to-zero-threshold":                 "10m",
			"tick-interval":                           "2s",
		},
		want: &Config{
			ContainerConcurrencyTargetPercentage: 0.5,
			ContainerConcurrencyTargetDefault:    10.0,
			MaxScaleUpRate:                       1.0,
			StableWindow:                         5 * time.Minute,
			PanicWindow:                          10 * time.Second,
			ScaleToZeroThreshold:                 10 * time.Minute,
			ScaleToZeroGracePeriod:               2 * time.Minute,
			ScaleToZeroIdlePeriod:                8 * time.Minute,
			TickInterval:                         2 * time.Second,
		},
	}, {
		name: "with explicit grace period",
		input: map[string]string{
			"enable-scale-to-zero":                    "false",
			"enable-vertical-pod-autoscaling":         "False",
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"scale-to-zero-threshold":                 "60s",
			"scale-to-zero-grace-period":              "30s",
			"tick-interval":                           "2s",
		},
		want: &Config{
			ContainerConcurrencyTargetPercentage: 0.5,
			ContainerConcurrencyTargetDefault:    10.0,
			MaxScaleUpRate:                       1.0,
			StableWindow:                         5 * time.Minute,
			PanicWindow:                          10 * time.Second,
			ScaleToZeroThreshold:                 60 * time.Second,
			ScaleToZeroGracePeriod:               30 * time.Second,
			ScaleToZeroIdlePeriod:                30 * time.Second,
			TickInterval:                         2 * time.Second,
		},
	}, {
		name: "missing required float field",
		input: map[string]string{
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"scale-to-zero-threshold":                 "10m",
			"tick-interval":                           "2s",
		},
		wantErr: true,
	}, {
		name: "missing required duration field",
		input: map[string]string{
			"max-scale-up-rate":                       "1.0",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"scale-to-zero-threshold":                 "10m",
			"tick-interval":                           "2s",
		},
		wantErr: true,
	}, {
		name: "malformed float",
		input: map[string]string{
			"max-scale-up-rate":                       "not a float",
			"container-concurrency-target-percentage": "0.5",
			"container-concurrency-target-default":    "10.0",
			"stable-window":                           "5m",
			"panic-window":                            "10s",
			"scale-to-zero-threshold":                 "10m",
			"tick-interval":                           "2s",
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
			"scale-to-zero-threshold":                 "10m",
			"tick-interval":                           "2s",
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
	b, err := ioutil.ReadFile(fmt.Sprintf("testdata/%s.yaml", ConfigName))
	if err != nil {
		t.Errorf("ReadFile() = %v", err)
	}
	var cm corev1.ConfigMap
	if err := yaml.Unmarshal(b, &cm); err != nil {
		t.Errorf("yaml.Unmarshal() = %v", err)
	}
	if _, err := NewConfigFromConfigMap(&cm); err != nil {
		t.Errorf("NewConfigFromConfigMap() = %v", err)
	}
}
