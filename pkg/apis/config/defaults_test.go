/*
Copyright 2019 The Knative Authors.

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
	if want := defaultDefaultsConfig(); !cmp.Equal(got, want) {
		t.Errorf("Example does not represent default config: diff(-want,+got)\n%s",
			cmp.Diff(want, got))
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
		},
		data: map[string]string{
			"revision-timeout-seconds":         "123",
			"max-revision-timeout-seconds":     "456",
			"revision-cpu-request":             "123m",
			"container-concurrency-max-limit":  "1984",
			"container-name-template":          "{{.Name}}",
			"allow-container-concurrency-zero": "false",
		},
	}, {
		name:    "invalid multi container flag value",
		wantErr: false,
		wantDefaults: &Defaults{
			RevisionTimeoutSeconds:        DefaultRevisionTimeoutSeconds,
			MaxRevisionTimeoutSeconds:     DefaultMaxRevisionTimeoutSeconds,
			UserContainerNameTemplate:     DefaultUserContainerName,
			ContainerConcurrencyMaxLimit:  DefaultMaxRevisionContainerConcurrency,
			AllowContainerConcurrencyZero: DefaultAllowContainerConcurrencyZero,
		},
		data: map[string]string{
			"enable-multi-container": "invalid",
		},
	}, {
		name:    "invalid allow container concurrency zero flag value",
		wantErr: false,
		wantDefaults: &Defaults{
			RevisionTimeoutSeconds:        DefaultRevisionTimeoutSeconds,
			MaxRevisionTimeoutSeconds:     DefaultMaxRevisionTimeoutSeconds,
			UserContainerNameTemplate:     DefaultUserContainerName,
			ContainerConcurrencyMaxLimit:  DefaultMaxRevisionContainerConcurrency,
			AllowContainerConcurrencyZero: false,
		},
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
			actualDefaults, err := NewDefaultsConfigFromConfigMap(&corev1.ConfigMap{
				Data: tt.data,
			})

			if (err != nil) != tt.wantErr {
				t.Fatalf("NewDefaultsConfigFromConfigMap() error = %v, WantErr %v", err, tt.wantErr)
			}

			if got, want := actualDefaults, tt.wantDefaults; !cmp.Equal(got, want) {
				t.Errorf("Config mismatch: diff(-want,+got):\n%s", cmp.Diff(want, got))
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
			def, err := NewDefaultsConfigFromConfigMap(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: system.Namespace(),
					Name:      DefaultsConfigName,
				},
				Data: map[string]string{
					"container-name-template": test.template,
				},
			})
			if err != nil {
				t.Fatal("Error parsing defaults:", err)
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
		if _, err := NewDefaultsConfigFromConfigMap(&corev1.ConfigMap{
			Data: map[string]string{
				"container-name-template": "{{animals-being-bros]]",
			},
		}); err == nil {
			t.Error("Expected an error but got none.")
		}
	})
}
