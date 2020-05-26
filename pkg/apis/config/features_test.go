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

package config

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	. "knative.dev/pkg/configmap/testing"
	_ "knative.dev/pkg/system/testing"
)

func TestFeaturesConfigurationFromFile(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, FeaturesConfigName)

	if _, err := NewFeaturesConfigFromConfigMap(cm); err != nil {
		t.Error("NewFeaturesConfigFromConfigMap(actual) =", err)
	}

	got, err := NewFeaturesConfigFromConfigMap(example)
	if err != nil {
		t.Fatal("NewFeaturesConfigFromConfigMap(example) =", err)
	}

	want := defaultFeaturesConfig()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error("Example does not represent default config: diff(-want,+got)\n", diff)
	}
}

func TestFeaturesConfiguration(t *testing.T) {
	configTests := []struct {
		name         string
		wantErr      bool
		wantFeatures *Features
		data         map[string]string
	}{{
		name:         "default configuration",
		wantErr:      false,
		wantFeatures: defaultFeaturesConfig(),
		data:         map[string]string{},
	}, {
		name:    "multi-container Allowed",
		wantErr: false,
		wantFeatures: &Features{
			MultiContainer: Allowed,
		},
		data: map[string]string{
			"multi-container": "Allowed",
		},
	}, {
		name:    "multi-container Enabled",
		wantErr: false,
		wantFeatures: &Features{
			MultiContainer: Enabled,
		},
		data: map[string]string{
			"multi-container": "Enabled",
		},
	}}

	for _, tt := range configTests {
		t.Run(tt.name, func(t *testing.T) {
			actualFeatures, err := NewFeaturesConfigFromConfigMap(&corev1.ConfigMap{
				Data: tt.data,
			})

			if (err != nil) != tt.wantErr {
				t.Fatalf("NewFeaturesConfigFromConfigMap() error = %v, WantErr %v", err, tt.wantErr)
			}

			got, want := actualFeatures, tt.wantFeatures
			if diff := cmp.Diff(want, got); diff != "" {
				t.Error("Config mismatch: diff(-want,+got):\n", diff)
			}
		})
	}
}
