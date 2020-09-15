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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	. "knative.dev/pkg/configmap/testing"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
	"knative.dev/serving/test/conformance/api/shared"
)

const defaultSidecarImage = "defaultImage"

func TestMatchingExceptions(t *testing.T) {
	cfg := defaultConfig()

	if delta := cfg.RegistriesSkippingTagResolving.Difference(shared.DigestResolutionExceptions); delta.Len() > 0 {
		t.Errorf("Got extra: %v", delta.List())
	}

	if delta := shared.DigestResolutionExceptions.Difference(cfg.RegistriesSkippingTagResolving); delta.Len() > 0 {
		t.Errorf("Didn't get: %v", delta.List())
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
			RegistriesSkippingTagResolving: sets.NewString("ko.local", ""),
			QueueSidecarImage:              defaultSidecarImage,
			QueueSidecarCPURequest:         &QueueSidecarCPURequestDefault,
			ProgressDeadline:               ProgressDeadlineDefault,
		},
		data: map[string]string{
			QueueSidecarImageKey:              defaultSidecarImage,
			registriesSkippingTagResolvingKey: "ko.local,,",
		},
	}, {
		name: "controller configuration good progress deadline",
		wantConfig: &Config{
			RegistriesSkippingTagResolving: sets.NewString("kind.local", "ko.local", "dev.local"),
			QueueSidecarImage:              defaultSidecarImage,
			QueueSidecarCPURequest:         &QueueSidecarCPURequestDefault,
			ProgressDeadline:               444 * time.Second,
		},
		data: map[string]string{
			QueueSidecarImageKey: defaultSidecarImage,
			ProgressDeadlineKey:  "444s",
		},
	}, {
		name: "controller configuration with registries",
		wantConfig: &Config{
			RegistriesSkippingTagResolving: sets.NewString("ko.local", "ko.dev"),
			QueueSidecarImage:              defaultSidecarImage,
			QueueSidecarCPURequest:         &QueueSidecarCPURequestDefault,
			ProgressDeadline:               ProgressDeadlineDefault,
		},
		data: map[string]string{
			QueueSidecarImageKey:              defaultSidecarImage,
			registriesSkippingTagResolvingKey: "ko.local,ko.dev",
		},
	}, {
		name: "controller configuration with custom queue sidecar resource request/limits",
		wantConfig: &Config{
			RegistriesSkippingTagResolving:      sets.NewString("kind.local", "ko.local", "dev.local"),
			QueueSidecarImage:                   defaultSidecarImage,
			ProgressDeadline:                    ProgressDeadlineDefault,
			QueueSidecarCPURequest:              resourcePtr(resource.MustParse("123m")),
			QueueSidecarMemoryRequest:           resourcePtr(resource.MustParse("456M")),
			QueueSidecarEphemeralStorageRequest: resourcePtr(resource.MustParse("789m")),
			QueueSidecarCPULimit:                resourcePtr(resource.MustParse("987M")),
			QueueSidecarMemoryLimit:             resourcePtr(resource.MustParse("654m")),
			QueueSidecarEphemeralStorageLimit:   resourcePtr(resource.MustParse("321M")),
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

func resourcePtr(q resource.Quantity) *resource.Quantity {
	return &q
}
