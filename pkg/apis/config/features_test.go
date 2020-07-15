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
	"reflect"
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
		name:    "features Enabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			MultiContainer:                Enabled,
			PodSpecDryRun:                 Enabled,
			PreventActiveRevisionDeletion: Enabled,
			ResponsiveRevisionGC:          Enabled,
		}),
		data: map[string]string{
			"multi-container":                  "Enabled",
			"kubernetes.podspec-dryrun":        "Enabled",
			"prevent-active-revision-deletion": "Enabled",
			"responsive-revision-gc":           "Enabled",
		},
	}, {
		name:    "multi-container Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			MultiContainer: Allowed,
		}),
		data: map[string]string{
			"multi-container": "Allowed",
		},
	}, {
		name:    "multi-container Disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			MultiContainer: Disabled,
		}),
		data: map[string]string{
			"multi-container": "Disabled",
		},
	}, {
		name:    "kubernetes.podspec-fieldref Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecFieldRef: Allowed,
		}),
		data: map[string]string{
			"kubernetes.podspec-fieldref": "Allowed",
		},
	}, {
		name:    "kubernetes.podspec-fieldref Enabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecFieldRef: Enabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-fieldref": "Enabled",
		},
	}, {
		name:    "kubernetes.podspec-fieldref Disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecFieldRef: Disabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-fieldref": "Disabled",
		},
	}, {
		name:    "kubernetes.podspec-dryrun Disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecDryRun: Disabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-dryrun": "Disabled",
		},
	}, {
		name:    "responsive-revision-gc Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			ResponsiveRevisionGC: Allowed,
		}),
		data: map[string]string{
			"responsive-revision-gc": "Allowed",
		},
	}, {
		name:    "responsive-revision-gc Enabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			ResponsiveRevisionGC: Enabled,
		}),
		data: map[string]string{
			"responsive-revision-gc": "Enabled",
		},
	}, {
		name:    "prevent-active-revision-deletion Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PreventActiveRevisionDeletion: Allowed,
		}),
		data: map[string]string{"prevent-active-revision-deletion": "Allowed"},
	}, {
		name:    "prevent-active-revision-deletion Enabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PreventActiveRevisionDeletion: Enabled,
		}),
		data: map[string]string{
			"prevent-active-revision-deletion": "Enabled",
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

// defaultWith returns the default *Feature patched with the provided *Features.
func defaultWith(p *Features) *Features {
	f := defaultFeaturesConfig()
	pType := reflect.ValueOf(p).Elem()
	fType := reflect.ValueOf(f).Elem()
	for i := 0; i < pType.NumField(); i++ {
		if pType.Field(i).Interface().(Flag) != "" {
			fType.Field(i).Set(pType.Field(i))
		}
	}
	return f
}
