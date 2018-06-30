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
	wantSingle := 3.14
	wantMulti := 1.618
	wantVPAMulti := 2.718
	c := &Config{
		SingleTargetConcurrency:   wantSingle,
		MultiTargetConcurrency:    wantMulti,
		VPAMultiTargetConcurrency: wantVPAMulti,
	}

	tests := []struct {
		name      string
		enableVPA bool
		model     v1alpha1.RevisionRequestConcurrencyModelType
		want      float64
	}{{
		name:      "multi with vpa",
		enableVPA: true,
		model:     v1alpha1.RevisionRequestConcurrencyModelMulti,
		want:      wantVPAMulti,
	}, {
		name:      "multi without vpa",
		enableVPA: false,
		model:     v1alpha1.RevisionRequestConcurrencyModelMulti,
		want:      wantMulti,
	}, {
		name:      "single with vpa",
		enableVPA: true,
		model:     v1alpha1.RevisionRequestConcurrencyModelSingle,
		want:      wantSingle,
	}, {
		name:      "single without vpa",
		enableVPA: false,
		model:     v1alpha1.RevisionRequestConcurrencyModelSingle,
		want:      wantSingle,
	}, {
		name:      "unknown mode defaults to multi",
		enableVPA: false,
		model:     "zomg",
		want:      wantMulti,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c.EnableVPA = test.enableVPA
			got := c.TargetConcurrency(test.model)
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
			"max-scale-up-rate":           "1.0",
			"single-concurrency-target":   "1.0",
			"multi-concurrency-target":    "1.0",
			"stable-window":               "5m",
			"panic-window":                "10s",
			"scale-to-zero-threshold":     "10m",
			"concurrency-quantum-of-time": "100ms",
		},
		want: &Config{
			SingleTargetConcurrency:   1.0,
			MultiTargetConcurrency:    1.0,
			VPAMultiTargetConcurrency: 10.0,
			MaxScaleUpRate:            1.0,
			StableWindow:              5 * time.Minute,
			PanicWindow:               10 * time.Second,
			ScaleToZeroThreshold:      10 * time.Minute,
			ConcurrencyQuantumOfTime:  100 * time.Millisecond,
		},
	}, {
		name: "with vpa specified",
		input: map[string]string{
			"max-scale-up-rate":            "1.0",
			"single-concurrency-target":    "1.0",
			"multi-concurrency-target":     "1.0",
			"vpa-multi-concurrency-target": "1.0",
			"stable-window":                "5m",
			"panic-window":                 "10s",
			"scale-to-zero-threshold":      "10m",
			"concurrency-quantum-of-time":  "100ms",
		},
		want: &Config{
			SingleTargetConcurrency:   1.0,
			MultiTargetConcurrency:    1.0,
			VPAMultiTargetConcurrency: 1.0, // not the default!
			MaxScaleUpRate:            1.0,
			StableWindow:              5 * time.Minute,
			PanicWindow:               10 * time.Second,
			ScaleToZeroThreshold:      10 * time.Minute,
			ConcurrencyQuantumOfTime:  100 * time.Millisecond,
		},
	}, {
		name: "with toggles on",
		input: map[string]string{
			"enable-scale-to-zero":            "true",
			"enable-vertical-pod-autoscaling": "true",
			"max-scale-up-rate":               "1.0",
			"single-concurrency-target":       "1.0",
			"multi-concurrency-target":        "1.0",
			"stable-window":                   "5m",
			"panic-window":                    "10s",
			"scale-to-zero-threshold":         "10m",
			"concurrency-quantum-of-time":     "100ms",
		},
		want: &Config{
			EnableScaleToZero:         true,
			EnableVPA:                 true,
			SingleTargetConcurrency:   1.0,
			MultiTargetConcurrency:    1.0,
			VPAMultiTargetConcurrency: 10.0,
			MaxScaleUpRate:            1.0,
			StableWindow:              5 * time.Minute,
			PanicWindow:               10 * time.Second,
			ScaleToZeroThreshold:      10 * time.Minute,
			ConcurrencyQuantumOfTime:  100 * time.Millisecond,
		},
	}, {
		name: "with toggles on strange casing",
		input: map[string]string{
			"enable-scale-to-zero":            "TRUE",
			"enable-vertical-pod-autoscaling": "True",
			"max-scale-up-rate":               "1.0",
			"single-concurrency-target":       "1.0",
			"multi-concurrency-target":        "1.0",
			"stable-window":                   "5m",
			"panic-window":                    "10s",
			"scale-to-zero-threshold":         "10m",
			"concurrency-quantum-of-time":     "100ms",
		},
		want: &Config{
			EnableScaleToZero:         true,
			EnableVPA:                 true,
			SingleTargetConcurrency:   1.0,
			MultiTargetConcurrency:    1.0,
			VPAMultiTargetConcurrency: 10.0,
			MaxScaleUpRate:            1.0,
			StableWindow:              5 * time.Minute,
			PanicWindow:               10 * time.Second,
			ScaleToZeroThreshold:      10 * time.Minute,
			ConcurrencyQuantumOfTime:  100 * time.Millisecond,
		},
	}, {
		name: "with toggles explicitly off",
		input: map[string]string{
			"enable-scale-to-zero":            "false",
			"enable-vertical-pod-autoscaling": "False",
			"max-scale-up-rate":               "1.0",
			"single-concurrency-target":       "1.0",
			"multi-concurrency-target":        "1.0",
			"stable-window":                   "5m",
			"panic-window":                    "10s",
			"scale-to-zero-threshold":         "10m",
			"concurrency-quantum-of-time":     "100ms",
		},
		want: &Config{
			SingleTargetConcurrency:   1.0,
			MultiTargetConcurrency:    1.0,
			VPAMultiTargetConcurrency: 10.0,
			MaxScaleUpRate:            1.0,
			StableWindow:              5 * time.Minute,
			PanicWindow:               10 * time.Second,
			ScaleToZeroThreshold:      10 * time.Minute,
			ConcurrencyQuantumOfTime:  100 * time.Millisecond,
		},
	}, {
		name: "missing required float field",
		input: map[string]string{
			"single-concurrency-target":   "1.0",
			"multi-concurrency-target":    "1.0",
			"stable-window":               "5m",
			"panic-window":                "10s",
			"scale-to-zero-threshold":     "10m",
			"concurrency-quantum-of-time": "100ms",
		},
		wantErr: true,
	}, {
		name: "missing required duration field",
		input: map[string]string{
			"max-scale-up-rate":           "1.0",
			"single-concurrency-target":   "1.0",
			"multi-concurrency-target":    "1.0",
			"stable-window":               "5m",
			"scale-to-zero-threshold":     "10m",
			"concurrency-quantum-of-time": "100ms",
		},
		wantErr: true,
	}, {
		name: "malformed float",
		input: map[string]string{
			"max-scale-up-rate":           "not a float",
			"single-concurrency-target":   "1.0",
			"multi-concurrency-target":    "1.0",
			"stable-window":               "5m",
			"panic-window":                "10s",
			"scale-to-zero-threshold":     "10m",
			"concurrency-quantum-of-time": "100ms",
		},
		wantErr: true,
	}, {
		name: "malformed duration",
		input: map[string]string{
			"max-scale-up-rate":           "1.0",
			"single-concurrency-target":   "1.0",
			"multi-concurrency-target":    "1.0",
			"stable-window":               "not a duration",
			"panic-window":                "10s",
			"scale-to-zero-threshold":     "10m",
			"concurrency-quantum-of-time": "100ms",
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
