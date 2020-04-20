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

package deployment

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	. "knative.dev/pkg/configmap/testing"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
)

const noSidecarImage = ""

func TestControllerConfigurationFromFile(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, ConfigName, QueueSidecarImageKey)

	if _, err := NewConfigFromConfigMap(cm); err != nil {
		t.Error("NewConfigFromConfigMap(actual) =", err)
	}

	if got, err := NewConfigFromConfigMap(example); err != nil {
		t.Error("NewConfigFromConfigMap(example) =", err)
	} else {
		want := defaultConfig()
		// We require QSI to be explicitly set. So do it here.
		want.QueueSidecarImage = "ko://knative.dev/serving/cmd/queue"
		if !cmp.Equal(got, want) {
			t.Error("Example stanza does not match default, diff(-want,+got):", cmp.Diff(want, got))
		}
	}
}

func TestControllerConfiguration(t *testing.T) {
	configTests := []struct {
		name       string
		wantErr    bool
		wantConfig *Config
		data       map[string]string
	}{{
		name: "controller configuration with bad registries",
		wantConfig: &Config{
			RegistriesSkippingTagResolving: sets.NewString("ko.local", ""),
			QueueSidecarImage:              noSidecarImage,
			ProgressDeadline:               ProgressDeadlineDefault,
		},
		data: map[string]string{
			QueueSidecarImageKey:              noSidecarImage,
			registriesSkippingTagResolvingKey: "ko.local,,",
		},
	}, {
		name: "controller configuration good progress deadline",
		wantConfig: &Config{
			RegistriesSkippingTagResolving: sets.NewString("ko.local", "dev.local"),
			QueueSidecarImage:              noSidecarImage,
			ProgressDeadline:               444 * time.Second,
		},
		data: map[string]string{
			QueueSidecarImageKey: noSidecarImage,
			progressDeadlineKey:  "444s",
		},
	}, {
		name: "controller configuration with registries",
		wantConfig: &Config{
			RegistriesSkippingTagResolving: sets.NewString("ko.local", "ko.dev"),
			QueueSidecarImage:              noSidecarImage,
			ProgressDeadline:               ProgressDeadlineDefault,
		},
		data: map[string]string{
			QueueSidecarImageKey:              noSidecarImage,
			registriesSkippingTagResolvingKey: "ko.local,ko.dev",
		},
	}, {
		name:    "controller with no side car image",
		wantErr: true,
		data:    map[string]string{},
	}, {
		name:    "controller configuration invalid progress deadline",
		wantErr: true,
		data: map[string]string{
			QueueSidecarImageKey: noSidecarImage,
			progressDeadlineKey:  "not-a-number",
		},
	}, {
		name:    "controller configuration invalid progress deadline II",
		wantErr: true,
		data: map[string]string{
			QueueSidecarImageKey: noSidecarImage,
			progressDeadlineKey:  "-21",
		},
	}, {
		name:    "controller configuration invalid progress deadline III",
		wantErr: true,
		data: map[string]string{
			QueueSidecarImageKey: noSidecarImage,
			progressDeadlineKey:  "0",
		},
	}}

	for _, tt := range configTests {
		t.Run(tt.name, func(t *testing.T) {
			gotConfig, err := NewConfigFromConfigMap(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: system.Namespace(),
					Name:      ConfigName,
				},
				Data: tt.data,
			})

			if (err != nil) != tt.wantErr {
				t.Fatalf("NewConfigFromConfigMap() error = %v, want: %v", err, tt.wantErr)
			}

			if got, want := gotConfig, tt.wantConfig; !cmp.Equal(got, want) {
				t.Error("Config mismatch, diff(-want,+got):", cmp.Diff(want, got))
			}
		})
	}
}
