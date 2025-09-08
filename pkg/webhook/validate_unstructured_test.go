/*
Copyright 2020 The Knative Authors

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

package webhook

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"knative.dev/pkg/apis"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	"knative.dev/pkg/logging"
	logtesting "knative.dev/pkg/logging/testing"
)

func TestUnstructuredValidation(t *testing.T) {
	tests := []struct {
		name   string
		data   map[string]interface{}
		want   string
		dryrun bool
	}{{
		name: "valid run latest",
		data: map[string]interface{}{
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"podspec": map[string]interface{}{
							"containers": map[string]interface{}{
								"image": "busybox",
							},
						},
					},
				},
			},
		},
		dryrun: true,
	}, {
		name: "no template",
		data: map[string]interface{}{
			"spec": map[string]interface{}{},
		},
		dryrun: true,
	}, {
		name: "invalid structure",
		data: map[string]interface{}{
			"spec": true, // Invalid, spec is expected to be a struct
		},
		dryrun: true,
		want:   "could not traverse nested spec.template field",
	}, {
		name: "no test annotation",
		data: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":        "valid",
				"namespace":   "foo",
				"annotations": map[string]interface{}{}, // Skip validation because no test annotation
			},
			"spec": map[string]interface{}{},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := fakekubeclient.With(context.Background())
			logger := logtesting.TestLogger(t)
			ctx = logging.WithLogger(ctx, logger)
			if test.dryrun {
				ctx = enableDryRun(ctx)
			}
			unstruct := &unstructured.Unstructured{}
			unstruct.SetUnstructuredContent(test.data)

			got := ValidateService(ctx, unstruct)
			if got == nil {
				if test.want != "" {
					t.Errorf("Validate got=nil, want=%q", test.want)
				}
			} else if !strings.Contains(got.Error(), test.want) {
				t.Errorf("Validate got=%q, want=%q", got.Error(), test.want)
			}
		})
	}
}

func TestDryRunFeatureFlag(t *testing.T) {
	om := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":        "valid",
			"namespace":   "foo",
			"annotations": map[string]interface{}{}, // Skip validation because no test annotation
		},
		"spec": true, // Invalid, spec is expected to be a struct
	}

	tests := []struct {
		name       string
		dryRunFlag bool
		data       map[string]interface{}
		want       string
	}{{
		name:       "enabled dry-run",
		dryRunFlag: true,
		data:       om,
		want:       "could not traverse nested spec.template field",
	}, {
		name:       "enabled with strict annotation",
		dryRunFlag: true,
		data: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "valid",
				"namespace": "foo",
			},
			"spec": true, // Invalid, spec is expected to be a struct
		},
		want: "could not traverse nested spec.template field",
	}, {
		name:       "disabled with enabled annotation",
		dryRunFlag: false,
		data: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "invalid",
				"namespace": "foo",
			},
			"spec": true, // Invalid, spec is expected to be a struct
		},
		want: "", // expect no error despite invalid data.
	}, {
		name:       "disabled with strict annotation",
		dryRunFlag: false,
		data: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "invalid",
				"namespace": "foo",
			},
			"spec": true, // Invalid, spec is expected to be a struct
		},
		want: "", // expect no error despite invalid data.
	}, {
		name:       "disabled dry-run",
		dryRunFlag: false,
		data:       om,
		want:       "", // expect no error despite invalid data.
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := fakekubeclient.With(context.Background())
			logger := logtesting.TestLogger(t)
			ctx = logging.WithLogger(ctx, logger)
			if test.dryRunFlag {
				ctx = enableDryRun(ctx)
			}

			unstruct := &unstructured.Unstructured{}
			unstruct.SetUnstructuredContent(test.data)

			got := ValidateService(ctx, unstruct)
			if got == nil {
				if test.want != "" {
					t.Errorf("Validate got=nil, want=%q", test.want)
				}
			} else if test.want == "" || !strings.Contains(got.Error(), test.want) {
				t.Errorf("Validate got=%q, want=%q", got.Error(), test.want)
			}
		})
	}
}

func enableDryRun(ctx context.Context) context.Context {
	return apis.WithDryRun(ctx)
}
