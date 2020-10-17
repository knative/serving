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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"knative.dev/pkg/apis"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	"knative.dev/pkg/logging"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/apis/config"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

var (
	validMetadata = map[string]interface{}{
		"name":      "valid",
		"namespace": "foo",
		"annotations": map[string]interface{}{
			"features.knative.dev/podspec-dryrun": "enabled",
		},
	}
)

func TestUnstructuredValidation(t *testing.T) {
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
			"spec":     true, // Invalid, spec is expected to be a struct
		},
		want: "could not traverse nested spec.template field",
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
		dryRunFlag config.Flag
		data       map[string]interface{}
		want       string
	}{{
		name:       "enabled dry-run",
		dryRunFlag: config.Enabled,
		data:       om,
		want:       "could not traverse nested spec.template field",
	}, {
		name:       "enabled with strict annotation",
		dryRunFlag: config.Enabled,
		data: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "valid",
				"namespace": "foo",
				"annotations": map[string]interface{}{
					"features.knative.dev/podspec-dryrun": "strict",
				},
			},
			"spec": true, // Invalid, spec is expected to be a struct
		},
		want: "could not traverse nested spec.template field",
	}, {
		name:       "disabled with enabled annotation",
		dryRunFlag: config.Disabled,
		data: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "invalid",
				"namespace": "foo",
				"annotations": map[string]interface{}{
					"features.knative.dev/podspec-dryrun": "enabled",
				},
			},
			"spec": true, // Invalid, spec is expected to be a struct
		},
		want: "", // expect no error despite invalid data.
	}, {
		name:       "disabled with strict annotation",
		dryRunFlag: config.Disabled,
		data: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "invalid",
				"namespace": "foo",
				"annotations": map[string]interface{}{
					"features.knative.dev/podspec-dryrun": "strict",
				},
			},
			"spec": true, // Invalid, spec is expected to be a struct
		},
		want: "", // expect no error despite invalid data.
	}, {
		name:       "disabled dry-run",
		dryRunFlag: config.Disabled,
		data:       om,
		want:       "", // expect no error despite invalid data.
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := fakekubeclient.With(context.Background())
			logger := logtesting.TestLogger(t)
			ctx = logging.WithLogger(ctx, logger)
			ctx = enableDryRun(ctx, test.dryRunFlag)

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

func TestSkipUpdate(t *testing.T) {
	validService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"features.knative.dev/podspec-dryrun": "enabled",
			},
		},
		Spec: v1.ServiceSpec{
			ConfigurationSpec: v1.ConfigurationSpec{
				Template: v1.RevisionTemplateSpec{
					Spec: v1.RevisionSpec{
						PodSpec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Image: "busybox",
							}},
						},
					},
				},
			},
		},
	}

	validServiceUns, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(validService)

	tests := []struct {
		name string
		new  map[string]interface{}
		old  *v1.Service
		want string
	}{{
		name: "valid_empty_old",
		new:  validServiceUns,
		old: &v1.Service{
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{
					Template: v1.RevisionTemplateSpec{},
				},
			},
		},
		want: "dry run failed with kubeclient error: spec.template",
	}, {
		name: "skip_identical_old",
		new:  validServiceUns,
		old:  validService,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := fakekubeclient.With(context.Background())
			failKubeCalls(ctx)
			ctx = logging.WithLogger(ctx, logtesting.TestLogger(t))
			ctx = apis.WithinUpdate(ctx, test.old)

			unstruct := &unstructured.Unstructured{}
			unstruct.SetUnstructuredContent(test.new)

			got := ValidateService(ctx, unstruct)
			if got == nil {
				if test.want != "" {
					t.Errorf("Validate got=nil, want=%q", test.want)
				}
			} else if got.Error() != test.want {
				t.Errorf("Validate got=%q, want=%q", got.Error(), test.want)
			}
		})
	}
}

func enableDryRun(ctx context.Context, flag config.Flag) context.Context {
	return config.ToContext(ctx, &config.Config{
		Features: &config.Features{
			PodSpecDryRun: flag,
		},
	})
}
