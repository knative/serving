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

package webhook

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	"knative.dev/pkg/logging"
	logtesting "knative.dev/pkg/logging/testing"
)

func TestServiceValidation(t *testing.T) {
	validMetadata := map[string]interface{}{
		"name":      "valid",
		"namespace": "foo",
		"annotations": map[string]interface{}{
			"features.knative.dev/podspec-dryrun": "enabled",
		},
	}

	tests := []struct {
		name string
		data map[string]interface{}
		want string
	}{{
		name: "valid run latest",
		data: map[string]interface{}{
			"metadata": validMetadata,
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
	}, {
		name: "no template",
		data: map[string]interface{}{
			"metadata": validMetadata,
			"spec":     map[string]interface{}{},
		},
	}, {
		name: "invalid structure",
		data: map[string]interface{}{
			"metadata": validMetadata,
			"spec":     true, // Invalid, spec is expcted to be a struct
		},
		want: "could not traverse nested spec.template field",
	}, {
		name: "no test anotation",
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

			unstruct := &unstructured.Unstructured{}
			unstruct.SetUnstructuredContent(test.data)

			got := ValidateRevisionTemplate(ctx, unstruct)
			if got == nil {
				if test.want != "" {
					t.Errorf("Validate got='%v', want='%v'", got, test.want)
				}
			} else if !strings.Contains(got.Error(), test.want) {
				t.Errorf("Validate got='%v', want='%v'", got.Error(), test.want)
			}
		})
	}
}
