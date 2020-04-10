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

package validation

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	"knative.dev/pkg/logging"
	logtesting "knative.dev/pkg/logging/testing"
)

func TestServiceValidation(t *testing.T) {
	tests := []struct {
		name string
		data map[string]interface{}
		want error
	}{{
		name: "valid run latest",
		data: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "valid",
				"namespace": "foo",
			},
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
		want: nil,
	}, {
		name: "no template",
		data: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "valid",
				"namespace": "foo",
			},
			"spec": map[string]interface{}{},
		},
		want: nil,
	}, {
		name: "invalid structure",
		data: map[string]interface{}{
			"namespace": map[string]interface{}{
				"name": "valid",
			},
			"spec": true, // Invalid, spec is expcted to be a struct
		},
		want: errors.New("could not traverse nested spec.template field: " +
			".spec.template accessor error: true is of the type bool, expected map[string]interface{}"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := fakekubeclient.With(context.Background())
			logger := logtesting.TestLogger(t)
			ctx = logging.WithLogger(ctx, logger)

			unstruct := &unstructured.Unstructured{}
			unstruct.SetUnstructuredContent(test.data)

			got := ExtraServiceValidation(ctx, unstruct)
			if (got != nil || test.want != nil) && !cmp.Equal(test.want.Error(), got.Error()) {
				t.Errorf("Validate (-want, +got) = %v",
					cmp.Diff(test.want.Error(), got.Error()))
			}
		})
	}
}
