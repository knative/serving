/*
Copyright 2019 The Knative Authors

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
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"

	. "knative.dev/pkg/configmap/testing"
	_ "knative.dev/pkg/system/testing"
)

func TestDefaultsConfigurationFromFile(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, DefaultsConfigName)

	if _, err := NewDefaultsConfigFromConfigMap(cm); err != nil {
		t.Error("NewDefaultsConfigFromConfigMap(actual) =", err)
	}

	got, err := NewDefaultsConfigFromConfigMap(example)
	if err != nil {
		t.Fatal("NewDefaultsConfigFromConfigMap(example) =", err)
	}

	// Those are in example, to show usage,
	// but default is nil, i.e. inheriting k8s.
	// So for this test we ignore those, but verify the other fields.
	got.RevisionCPULimit, got.RevisionCPURequest = nil, nil
	got.RevisionMemoryLimit, got.RevisionMemoryRequest = nil, nil
	got.RevisionEphemeralStorageLimit, got.RevisionEphemeralStorageRequest = nil, nil
	want := defaultDefaultsConfig()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error("Example does not represent default config: diff(-want,+got)\n", diff)
	}
}

func TestDefaultsConfiguration(t *testing.T) {
	oneTwoThree := resource.MustParse("123m")

	configTests := []struct {
		name         string
		wantErr      bool
		wantDefaults *Defaults
		data         map[string]string
	}{{
		name:         "default configuration",
		wantErr:      false,
		wantDefaults: defaultDefaultsConfig(),
		data:         map[string]string{},
	}, {
		name:    "specified values",
		wantErr: false,
		wantDefaults: &Defaults{
			RevisionTimeoutSeconds:       123,
			MaxRevisionTimeoutSeconds:    456,
			ContainerConcurrencyMaxLimit: 1984,
			RevisionCPURequest:           &oneTwoThree,
			UserContainerNameTemplate:    "{{.Name}}",
			EnableServiceLinks:           ptr.Bool(true),
		},
		data: map[string]string{
			"revision-timeout-seconds":         "123",
			"max-revision-timeout-seconds":     "456",
			"revision-cpu-request":             "123m",
			"container-concurrency-max-limit":  "1984",
			"container-name-template":          "{{.Name}}",
			"allow-container-concurrency-zero": "false",
			"enable-service-links":             "true",
		},
	}, {
		name:    "service links false",
		wantErr: false,
		wantDefaults: &Defaults{
			RevisionTimeoutSeconds:        DefaultRevisionTimeoutSeconds,
			MaxRevisionTimeoutSeconds:     DefaultMaxRevisionTimeoutSeconds,
			UserContainerNameTemplate:     DefaultUserContainerName,
			ContainerConcurrencyMaxLimit:  DefaultMaxRevisionContainerConcurrency,
			AllowContainerConcurrencyZero: true,
			EnableServiceLinks:            ptr.Bool(false),
		},
		data: map[string]string{
			"enable-service-links": "false",
		},
	}, {
		name:    "service links default",
		wantErr: false,
		wantDefaults: &Defaults{
			RevisionTimeoutSeconds:        DefaultRevisionTimeoutSeconds,
			MaxRevisionTimeoutSeconds:     DefaultMaxRevisionTimeoutSeconds,
			UserContainerNameTemplate:     DefaultUserContainerName,
			ContainerConcurrencyMaxLimit:  DefaultMaxRevisionContainerConcurrency,
			AllowContainerConcurrencyZero: true,
			EnableServiceLinks:            nil,
		},
		data: map[string]string{
			"enable-service-links": "default",
		},
	}, {
		name:    "invalid allow container concurrency zero flag value",
		wantErr: true,
		data: map[string]string{
			"allow-container-concurrency-zero": "invalid",
		},
	}, {
		name:    "bad revision timeout",
		wantErr: true,
		data: map[string]string{
			"revision-timeout-seconds": "asdf",
		},
	}, {
		name:    "bad max revision timeout",
		wantErr: true,
		data: map[string]string{
			"max-revision-timeout-seconds": "asdf",
		},
	}, {
		name:    "bad name template",
		wantErr: true,
		data: map[string]string{
			"container-name-template": "{{.NAme}}",
		},
	}, {
		name:    "bad resource",
		wantErr: true,
		data: map[string]string{
			"revision-cpu-request": "bad",
		},
	}, {
		name:    "revision timeout bigger than max timeout",
		wantErr: true,
		data: map[string]string{
			"revision-timeout-seconds":     "456",
			"max-revision-timeout-seconds": "123",
		},
	}, {
		name:    "container-concurrency is bigger than default DefaultMaxRevisionContainerConcurrency",
		wantErr: true,
		data: map[string]string{
			"container-concurrency": "2000",
		},
	}, {
		name:         "container-concurrency is bigger than specified DefaultMaxRevisionContainerConcurrency",
		wantErr:      true,
		wantDefaults: (*Defaults)(nil),
		data: map[string]string{
			"container-concurrency":           "11",
			"container-concurrency-max-limit": "10",
		},
	}, {
		name:    "container-concurrency-max-limit is invalid",
		wantErr: true,
		data: map[string]string{
			"container-concurrency-max-limit": "0",
		},
	}}

	for _, tt := range configTests {
		t.Run(tt.name, func(t *testing.T) {
			actualDefaultsCM, err := NewDefaultsConfigFromConfigMap(&corev1.ConfigMap{
				Data: tt.data,
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewDefaultsConfigFromConfigMap() error = %v, WantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(actualDefaultsCM, tt.wantDefaults); diff != "" {
				t.Errorf("Config mismatch: diff(-want,+got):\n%s", diff)
			}

			actualDefaults, err := NewDefaultsConfigFromMap(tt.data)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewDefaultsConfigFromMap() error = %v, WantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(actualDefaults, actualDefaultsCM); diff != "" {
				t.Errorf("Config mismatch: diff(-want,+got):\n%s", diff)
			}
		})
	}
}

func TestTemplating(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     string
	}{{
		name:     "groot",
		template: "{{.Name}}",
		want:     "i-am-groot",
	}, {
		name:     "complex",
		template: "{{.Namespace}}-of-the-galaxy",
		want:     "guardians-of-the-galaxy",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: system.Namespace(),
					Name:      DefaultsConfigName,
				},
				Data: map[string]string{
					"container-name-template": test.template,
				},
			}
			defCM, err := NewDefaultsConfigFromConfigMap(configMap)
			if err != nil {
				t.Fatal("Error parsing defaults:", err)
			}

			def, err := NewDefaultsConfigFromMap(configMap.Data)
			if err != nil {
				t.Error("Error parsing defaults:", err)
			}

			if diff := cmp.Diff(defCM, def); diff != "" {
				t.Errorf("Config mismatch: diff(-want,+got):\n%s", diff)
			}

			ctx := apis.WithinParent(context.Background(), metav1.ObjectMeta{
				Name:      "i-am-groot",
				Namespace: "guardians",
			})

			if got, want := def.UserContainerName(ctx), test.want; got != want {
				t.Errorf("UserContainerName() = %v, wanted %v", got, want)
			}
		})
	}
	t.Run("bad-template", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			Data: map[string]string{
				"container-name-template": "{{animals-being-bros]]",
			},
		}
		_, err := NewDefaultsConfigFromConfigMap(configMap)
		if err == nil {
			t.Error("Expected an error but got none.")
		}
		_, err = NewDefaultsConfigFromMap(configMap.Data)
		if err == nil {
			t.Error("Expected an error but got none.")
		}
	})
}
