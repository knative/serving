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

package deployment

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/pkg/system"
	"knative.dev/serving/test/conformance/api/shared"

	. "knative.dev/pkg/configmap/testing"
	_ "knative.dev/pkg/system/testing"
)

const defaultSidecarImage = "defaultImage"

func TestMatchingExceptions(t *testing.T) {
	cfg := defaultConfig()

	if delta := cfg.RegistriesSkippingTagResolving.Difference(shared.DigestResolutionExceptions); delta.Len() > 0 {
		t.Error("Got extra:", sets.List(delta))
	}

	if delta := shared.DigestResolutionExceptions.Difference(cfg.RegistriesSkippingTagResolving); delta.Len() > 0 {
		t.Error("Didn't get:", sets.List(delta))
	}
}

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

		// The following are in the example yaml, to show usage,
		// but default is nil, i.e. inheriting k8s.
		// So for this test we ignore those, but verify the other fields.
		got.QueueSidecarCPULimit = nil
		got.QueueSidecarMemoryRequest, got.QueueSidecarMemoryLimit = nil, nil
		got.QueueSidecarEphemeralStorageRequest, got.QueueSidecarEphemeralStorageLimit = nil, nil
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
			RegistriesSkippingTagResolving: sets.New("ko.local", ""),
			DigestResolutionTimeout:        digestResolutionTimeoutDefault,
			QueueSidecarImage:              defaultSidecarImage,
			QueueSidecarCPURequest:         &QueueSidecarCPURequestDefault,
			QueueSidecarTokenAudiences:     sets.New("foo", "bar", "boo-srv"),
			ProgressDeadline:               ProgressDeadlineDefault,
		},
		data: map[string]string{
			QueueSidecarImageKey:              defaultSidecarImage,
			queueSidecarTokenAudiencesKey:     "bar,foo,boo-srv",
			registriesSkippingTagResolvingKey: "ko.local,,",
		},
	}, {
		name: "controller configuration good progress deadline",
		wantConfig: &Config{
			RegistriesSkippingTagResolving: sets.New("kind.local", "ko.local", "dev.local"),
			DigestResolutionTimeout:        digestResolutionTimeoutDefault,
			QueueSidecarImage:              defaultSidecarImage,
			QueueSidecarCPURequest:         &QueueSidecarCPURequestDefault,
			QueueSidecarTokenAudiences:     sets.New(""),
			ProgressDeadline:               444 * time.Second,
		},
		data: map[string]string{
			QueueSidecarImageKey: defaultSidecarImage,
			ProgressDeadlineKey:  "444s",
		},
	}, {
		name: "controller configuration good digest resolution timeout",
		wantConfig: &Config{
			RegistriesSkippingTagResolving: sets.New("kind.local", "ko.local", "dev.local"),
			DigestResolutionTimeout:        60 * time.Second,
			QueueSidecarImage:              defaultSidecarImage,
			QueueSidecarCPURequest:         &QueueSidecarCPURequestDefault,
			QueueSidecarTokenAudiences:     sets.New(""),
			ProgressDeadline:               ProgressDeadlineDefault,
		},
		data: map[string]string{
			QueueSidecarImageKey:       defaultSidecarImage,
			digestResolutionTimeoutKey: "60s",
		},
	}, {
		name: "controller configuration with registries",
		wantConfig: &Config{
			RegistriesSkippingTagResolving: sets.New("ko.local", "ko.dev"),
			DigestResolutionTimeout:        digestResolutionTimeoutDefault,
			QueueSidecarImage:              defaultSidecarImage,
			QueueSidecarCPURequest:         &QueueSidecarCPURequestDefault,
			QueueSidecarTokenAudiences:     sets.New(""),
			ProgressDeadline:               ProgressDeadlineDefault,
		},
		data: map[string]string{
			QueueSidecarImageKey:              defaultSidecarImage,
			registriesSkippingTagResolvingKey: "ko.local,ko.dev",
		},
	}, {
		name: "controller configuration with custom queue sidecar resource request/limits",
		wantConfig: &Config{
			RegistriesSkippingTagResolving:      sets.New("kind.local", "ko.local", "dev.local"),
			DigestResolutionTimeout:             digestResolutionTimeoutDefault,
			QueueSidecarImage:                   defaultSidecarImage,
			ProgressDeadline:                    ProgressDeadlineDefault,
			QueueSidecarCPURequest:              quantity("123m"),
			QueueSidecarMemoryRequest:           quantity("456M"),
			QueueSidecarEphemeralStorageRequest: quantity("789m"),
			QueueSidecarCPULimit:                quantity("987M"),
			QueueSidecarMemoryLimit:             quantity("654m"),
			QueueSidecarEphemeralStorageLimit:   quantity("321M"),
			QueueSidecarTokenAudiences:          sets.New(""),
		},
		data: map[string]string{
			QueueSidecarImageKey:                   defaultSidecarImage,
			queueSidecarCPURequestKey:              "123m",
			queueSidecarMemoryRequestKey:           "456M",
			queueSidecarEphemeralStorageRequestKey: "789m",
			queueSidecarCPULimitKey:                "987M",
			queueSidecarMemoryLimitKey:             "654m",
			queueSidecarEphemeralStorageLimitKey:   "321M",
		},
	}, {
		name:    "controller with no side car image",
		wantErr: true,
		data:    map[string]string{},
	}, {
		name:    "controller configuration invalid digest resolution timeout",
		wantErr: true,
		data: map[string]string{
			QueueSidecarImageKey:       defaultSidecarImage,
			digestResolutionTimeoutKey: "-1s",
		},
	}, {
		name:    "controller configuration invalid progress deadline",
		wantErr: true,
		data: map[string]string{
			QueueSidecarImageKey: defaultSidecarImage,
			ProgressDeadlineKey:  "not-a-duration",
		},
	}, {
		name:    "controller configuration invalid progress deadline II",
		wantErr: true,
		data: map[string]string{
			QueueSidecarImageKey: defaultSidecarImage,
			ProgressDeadlineKey:  "-21s",
		},
	}, {
		name:    "controller configuration invalid progress deadline III",
		wantErr: true,
		data: map[string]string{
			QueueSidecarImageKey: defaultSidecarImage,
			ProgressDeadlineKey:  "0ms",
		},
	}, {
		name:    "controller configuration invalid progress deadline IV",
		wantErr: true,
		data: map[string]string{
			QueueSidecarImageKey: defaultSidecarImage,
			ProgressDeadlineKey:  "1982ms",
		},
	}, {
		name: "legacy keys supported",
		data: map[string]string{
			// Legacy keys for backwards compatibility
			"queueSidecarImage":                   "1",
			"progressDeadline":                    "2s",
			"digestResolutionTimeout":             "3s",
			"registriesSkippingTagResolving":      "4",
			"queueSidecarCPURequest":              "5m",
			"queueSidecarCPULimit":                "6m",
			"queueSidecarMemoryRequest":           "7M",
			"queueSidecarMemoryLimit":             "8M",
			"queueSidecarEphemeralStorageRequest": "9M",
			"queueSidecarEphemeralStorageLimit":   "10M",
		},
		wantConfig: &Config{
			QueueSidecarImage:                   "1",
			ProgressDeadline:                    2 * time.Second,
			DigestResolutionTimeout:             3 * time.Second,
			RegistriesSkippingTagResolving:      sets.New("4"),
			QueueSidecarCPURequest:              quantity("5m"),
			QueueSidecarCPULimit:                quantity("6m"),
			QueueSidecarMemoryRequest:           quantity("7M"),
			QueueSidecarMemoryLimit:             quantity("8M"),
			QueueSidecarEphemeralStorageRequest: quantity("9M"),
			QueueSidecarEphemeralStorageLimit:   quantity("10M"),
			QueueSidecarTokenAudiences:          sets.New(""),
		},
	}, {
		name: "newer key case takes priority",
		data: map[string]string{
			// Legacy keys for backwards compatibility
			"queueSidecarImage":                   "1",
			"progressDeadline":                    "2s",
			"digestResolutionTimeout":             "3s",
			"registriesSkippingTagResolving":      "4",
			"queueSidecarCPURequest":              "5m",
			"queueSidecarCPULimit":                "6m",
			"queueSidecarMemoryRequest":           "7M",
			"queueSidecarMemoryLimit":             "8M",
			"queueSidecarEphemeralStorageRequest": "9M",
			"queueSidecarEphemeralStorageLimit":   "10M",
			"queueSidecarTokens":                  "bar",

			QueueSidecarImageKey:                   "12",
			ProgressDeadlineKey:                    "13s",
			digestResolutionTimeoutKey:             "14s",
			registriesSkippingTagResolvingKey:      "15",
			queueSidecarCPURequestKey:              "16m",
			queueSidecarCPULimitKey:                "17m",
			queueSidecarMemoryRequestKey:           "18M",
			queueSidecarMemoryLimitKey:             "19M",
			queueSidecarEphemeralStorageRequestKey: "20M",
			queueSidecarEphemeralStorageLimitKey:   "21M",
			queueSidecarTokenAudiencesKey:          "foo",
		},
		wantConfig: &Config{
			QueueSidecarImage:                   "12",
			ProgressDeadline:                    13 * time.Second,
			DigestResolutionTimeout:             14 * time.Second,
			RegistriesSkippingTagResolving:      sets.New("15"),
			QueueSidecarCPURequest:              quantity("16m"),
			QueueSidecarCPULimit:                quantity("17m"),
			QueueSidecarMemoryRequest:           quantity("18M"),
			QueueSidecarMemoryLimit:             quantity("19M"),
			QueueSidecarEphemeralStorageRequest: quantity("20M"),
			QueueSidecarEphemeralStorageLimit:   quantity("21M"),
			QueueSidecarTokenAudiences:          sets.New("foo"),
		},
	}}

	for _, tt := range configTests {
		t.Run(tt.name, func(t *testing.T) {
			gotConfigCM, err := NewConfigFromConfigMap(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: system.Namespace(),
					Name:      ConfigName,
				},
				Data: tt.data,
			})

			if (err != nil) != tt.wantErr {
				t.Fatalf("NewConfigFromConfigMap() error = %v, want: %v", err, tt.wantErr)
			}

			if got, want := gotConfigCM, tt.wantConfig; !cmp.Equal(got, want) {
				t.Error("Config mismatch, diff(-want,+got):", cmp.Diff(want, got))
			}

			gotConfig, err := NewConfigFromMap(tt.data)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewConfigFromMap() error = %v, WantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(gotConfig, gotConfigCM); diff != "" {
				t.Fatalf("Config mismatch: diff(-want,+got):\n%s", diff)
			}
		})
	}
}

func quantity(val string) *resource.Quantity {
	r := resource.MustParse(val)
	return &r
}
