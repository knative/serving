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

package serving

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/config"
	routeconfig "knative.dev/serving/pkg/reconciler/route/config"
)

func TestValidateObjectMetadata(t *testing.T) {
	cases := []struct {
		name       string
		objectMeta metav1.Object
		expectErr  error
	}{{
		name: "invalid name - dots",
		objectMeta: &metav1.ObjectMeta{
			Name: "do.not.use.dots",
		},
		expectErr: &apis.FieldError{
			Message: "not a DNS 1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			Paths:   []string{"name"},
		},
	}, {
		name: "invalid name - too long",
		objectMeta: &metav1.ObjectMeta{
			Name: strings.Repeat("a", 64),
		},
		expectErr: &apis.FieldError{
			Message: "not a DNS 1035 label: [must be no more than 63 characters]",
			Paths:   []string{"name"},
		},
	}, {
		name: "invalid name - trailing dash",
		objectMeta: &metav1.ObjectMeta{
			Name: "some-name-",
		},
		expectErr: &apis.FieldError{
			Message: "not a DNS 1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			Paths:   []string{"name"},
		},
	}, {
		name: "valid generateName",
		objectMeta: &metav1.ObjectMeta{
			GenerateName: "some-name",
		},
		expectErr: (*apis.FieldError)(nil),
	}, {
		name: "valid generateName - trailing dash",
		objectMeta: &metav1.ObjectMeta{
			GenerateName: "some-name-",
		},
		expectErr: (*apis.FieldError)(nil),
	}, {
		name: "invalid generateName - dots",
		objectMeta: &metav1.ObjectMeta{
			GenerateName: "do.not.use.dots",
		},
		expectErr: &apis.FieldError{
			Message: "not a DNS 1035 label prefix: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			Paths:   []string{"generateName"},
		},
	}, {
		name: "invalid generateName - too long",
		objectMeta: &metav1.ObjectMeta{
			GenerateName: strings.Repeat("a", 64),
		},
		expectErr: &apis.FieldError{
			Message: "not a DNS 1035 label prefix: [must be no more than 63 characters]",
			Paths:   []string{"generateName"},
		},
	}, {
		name:       "missing name and generateName",
		objectMeta: &metav1.ObjectMeta{},
		expectErr: &apis.FieldError{
			Message: "name or generateName is required",
			Paths:   []string{"name"},
		},
	}, {
		name: "valid forceUpgrade annotation label",
		objectMeta: &metav1.ObjectMeta{
			GenerateName: "some-name",
			Annotations: map[string]string{
				"serving.knative.dev/forceUpgrade": "true",
			},
		},
		expectErr: (*apis.FieldError)(nil),
	}, {
		name: "valid creator annotation label",
		objectMeta: &metav1.ObjectMeta{
			GenerateName: "some-name",
			Annotations: map[string]string{
				CreatorAnnotation: "svc-creator",
			},
		},

		expectErr: (*apis.FieldError)(nil),
	}, {
		name: "valid lastModifier annotation label",
		objectMeta: &metav1.ObjectMeta{
			GenerateName: "some-name",
			Annotations: map[string]string{
				UpdaterAnnotation: "svc-modifier",
			},
		},
		expectErr: (*apis.FieldError)(nil),
	}, {
		name: "valid lastPinned annotation label",
		objectMeta: &metav1.ObjectMeta{
			GenerateName: "some-name",
			Annotations: map[string]string{
				RevisionLastPinnedAnnotationKey: "pinned-val",
			},
		},
		expectErr: (*apis.FieldError)(nil),
	}, {
		name: "invalid knative prefix annotation",
		objectMeta: &metav1.ObjectMeta{
			GenerateName: "some-name",
			Annotations: map[string]string{
				"serving.knative.dev/testAnnotation": "value",
			},
		},
		expectErr: (&apis.FieldError{Message: "", Paths: []string(nil), Details: ""}).Also(
			(&apis.FieldError{Message: "", Paths: []string(nil), Details: ""}).Also(
				(&apis.FieldError{Message: "", Paths: []string(nil), Details: ""}).Also(
					apis.ErrInvalidKeyName("serving.knative.dev/testAnnotation", "annotations"),
				))),
	}, {
		name: "valid non-knative prefix annotation label",
		objectMeta: &metav1.ObjectMeta{
			GenerateName: "some-name",
			Annotations: map[string]string{
				"testAnnotation": "testValue",
			},
		},
		expectErr: (*apis.FieldError)(nil),
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateObjectMetadata(c.objectMeta)

			if !reflect.DeepEqual(c.expectErr, err) {
				t.Errorf("Expected: '%#v', Got: '%#v'", c.expectErr, err)
			}
		})
	}
}

func TestValidateQueueSidecarAnnotation(t *testing.T) {
	cases := []struct {
		name       string
		annotation map[string]string
		expectErr  error
	}{{
		name: "Queue sidecar resource percentage annotation more than 100",
		annotation: map[string]string{
			QueueSideCarResourcePercentageAnnotation: "200",
		},
		expectErr: &apis.FieldError{
			Message: "expected 0.1 <= 200 <= 100",
			Paths:   []string{QueueSideCarResourcePercentageAnnotation},
		},
	}, {
		name: "Invalid queue sidecar resource percentage annotation",
		annotation: map[string]string{
			QueueSideCarResourcePercentageAnnotation: "",
		},
		expectErr: &apis.FieldError{
			Message: "invalid value: ",
			Paths:   []string{fmt.Sprintf("[%s]", QueueSideCarResourcePercentageAnnotation)},
		},
	}, {
		name:       "empty annotation",
		annotation: map[string]string{},
		expectErr:  (*apis.FieldError)(nil),
	}, {
		name: "different annotation other than QueueSideCarResourcePercentageAnnotation",
		annotation: map[string]string{
			CreatorAnnotation: "",
		},
		expectErr: (*apis.FieldError)(nil),
	}, {
		name: "valid value for Queue sidecar resource percentage annotation",
		annotation: map[string]string{
			QueueSideCarResourcePercentageAnnotation: "100",
		},
		expectErr: (*apis.FieldError)(nil),
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateQueueSidecarAnnotation(c.annotation)
			if !reflect.DeepEqual(c.expectErr, err) {
				t.Errorf("Expected: '%#v', Got: '%#v'", c.expectErr, err)
			}
		})
	}
}

func TestValidateTimeoutSecond(t *testing.T) {
	cases := []struct {
		name      string
		timeout   *int64
		expectErr error
	}{{
		name:    "exceed max timeout",
		timeout: ptr.Int64(6000),
		expectErr: apis.ErrOutOfBoundsValue(
			6000, 0, config.DefaultMaxRevisionTimeoutSeconds,
			"timeoutSeconds"),
	}, {
		name:      "valid timeout value",
		timeout:   ptr.Int64(100),
		expectErr: (*apis.FieldError)(nil),
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			err := ValidateTimeoutSeconds(ctx, *c.timeout)
			if !reflect.DeepEqual(c.expectErr, err) {
				t.Errorf("Expected: '%#v', Got: '%#v'", c.expectErr, err)
			}
		})
	}
}

func TestValidateContainerConcurrency(t *testing.T) {
	cases := []struct {
		name                 string
		containerConcurrency *int64
		expectErr            error
	}{{
		name:                 "empty containerConcurrency",
		containerConcurrency: nil,
		expectErr:            (*apis.FieldError)(nil),
	}, {
		name:                 "invalid containerConcurrency value",
		containerConcurrency: ptr.Int64(2000),
		expectErr: apis.ErrOutOfBoundsValue(
			2000, 0, config.DefaultMaxRevisionContainerConcurrency, apis.CurrentField),
	}, {
		name:                 "valid containerConcurrency value",
		containerConcurrency: ptr.Int64(10),
		expectErr:            (*apis.FieldError)(nil),
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateContainerConcurrency(c.containerConcurrency)
			if !reflect.DeepEqual(c.expectErr, err) {
				t.Errorf("Expected: '%#v', Got: '%#v'", c.expectErr, err)
			}
		})
	}
}

func TestValidateClusterVisibilityLabel(t *testing.T) {
	tests := []struct {
		name      string
		label     string
		expectErr error
	}{{
		name:      "empty label",
		label:     "",
		expectErr: apis.ErrInvalidValue("", routeconfig.VisibilityLabelKey),
	}, {
		name:      "valid label",
		label:     routeconfig.VisibilityClusterLocal,
		expectErr: (*apis.FieldError)(nil),
	}, {
		name:      "invalid label",
		label:     "not-cluster-local",
		expectErr: apis.ErrInvalidValue("not-cluster-local", routeconfig.VisibilityLabelKey),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateClusterVisibilityLabel(test.label)
			if !reflect.DeepEqual(test.expectErr, err) {
				t.Errorf("ValidateClusterVisibilityLabel(%s) = %#v, Want: '%#v'", test.label, err, test.expectErr)
			}
		})
	}

}

type withPod struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              corev1.PodSpec `json:"spec,omitempty"`
}

func getSpec(image string) corev1.PodSpec {
	return corev1.PodSpec{
		Containers: []corev1.Container{{
			Image: image,
		}},
	}
}

func TestAnnotationCreate(t *testing.T) {
	const (
		u1 = "oveja@knative.dev"
		u2 = "cabra@knative.dev"
	)
	tests := []struct {
		name string
		user string
		this *withPod
		want map[string]string
	}{{
		name: "create annotation",
		user: u1,
		this: &withPod{
			Spec: getSpec("foo"),
		},
		want: map[string]string{
			CreatorAnnotation: u1,
			UpdaterAnnotation: u1,
		},
	}, {
		name: "create annotation should override user provided annotations",
		user: u1,
		this: &withPod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					CreatorAnnotation: u2,
					UpdaterAnnotation: u2,
				},
			},
			Spec: getSpec("foo"),
		},
		want: map[string]string{
			CreatorAnnotation: u1,
			UpdaterAnnotation: u1,
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := apis.WithUserInfo(context.Background(), &authv1.UserInfo{
				Username: test.user,
			})
			SetUserInfo(ctx, nil, test.this.Spec, test.this)
			if !reflect.DeepEqual(test.this.Annotations, test.want) {
				t.Errorf("Annotations = %v, want: %v, diff (-got, +want): %s", test.this.Annotations, test.want, cmp.Diff(test.this.Annotations, test.want))
			}
		})
	}
}

func TestAnnotationUpdate(t *testing.T) {
	const (
		u1 = "oveja@knative.dev"
		u2 = "cabra@knative.dev"
	)
	tests := []struct {
		name string
		user string
		prev *withPod
		this *withPod
		want map[string]string
	}{{
		name: "update annotation without spec changes",
		user: u2,
		this: &withPod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					CreatorAnnotation: u1,
					UpdaterAnnotation: u1,
				},
			},
			Spec: getSpec("foo"),
		},
		prev: &withPod{
			Spec: getSpec("foo"),
		},
		want: map[string]string{
			CreatorAnnotation: u1,
			UpdaterAnnotation: u1,
		},
	}, {
		name: "update annotation with spec changes",
		user: u2,
		this: &withPod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					CreatorAnnotation: u1,
					UpdaterAnnotation: u1,
				},
			},
			Spec: getSpec("bar"),
		},
		prev: &withPod{
			Spec: getSpec("foo"),
		},
		want: map[string]string{
			CreatorAnnotation: u1,
			UpdaterAnnotation: u2,
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := apis.WithUserInfo(context.Background(), &authv1.UserInfo{
				Username: test.user,
			})
			if test.prev != nil {
				ctx = apis.WithinUpdate(ctx, test.prev)
			}
			SetUserInfo(ctx, test.prev.Spec, test.this.Spec, test.this)
			if !reflect.DeepEqual(test.this.Annotations, test.want) {
				t.Errorf("Annotations = %v, want: %v, diff (-got, +want): %s", test.this.Annotations, test.want, cmp.Diff(test.this.Annotations, test.want))
			}
		})
	}
}

func TestValidateRevisionName(t *testing.T) {
	cases := []struct {
		name            string
		revName         string
		revGenerateName string
		objectMeta      metav1.ObjectMeta
		expectErr       error
	}{{
		name:            "invalid revision generateName - dots",
		revGenerateName: "foo.bar",
		expectErr: apis.ErrInvalidValue("not a DNS 1035 label prefix: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			"metadata.generateName"),
	}, {
		name:    "invalid revision name - dots",
		revName: "foo.bar",
		expectErr: apis.ErrInvalidValue("not a DNS 1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			"metadata.name"),
	}, {
		name: "invalid name (not prefixed)",
		objectMeta: metav1.ObjectMeta{
			Name: "bar",
		},
		revName: "foo",
		expectErr: apis.ErrInvalidValue(`"foo" must have prefix "bar-"`,
			"metadata.name"),
	}, {
		name: "invalid name (with generateName)",
		objectMeta: metav1.ObjectMeta{
			GenerateName: "foo-bar-",
		},
		revName:   "foo-bar-foo",
		expectErr: apis.ErrDisallowedFields("metadata.name"),
	}, {
		name: "valid name",
		objectMeta: metav1.ObjectMeta{
			Name: "valid",
		},
		revName:   "valid-name",
		expectErr: (*apis.FieldError)(nil),
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinParent(ctx, c.objectMeta)
			if err := ValidateRevisionName(ctx, c.revName, c.revGenerateName); !reflect.DeepEqual(c.expectErr, err) {
				t.Errorf("Expected: '%#v', Got: '%#v'", c.expectErr, err)
			}
		})
	}
}
