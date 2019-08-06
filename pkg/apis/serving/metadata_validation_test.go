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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/config"
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

func TestAnnotationValidation(t *testing.T) {
	cases := []struct {
		name        string
		annotations map[string]string
		expectErr   *apis.FieldError
	}{{
		name: "valid creator annotation label",
		annotations: map[string]string{
			"serving.knative.dev/creator": "svc-creator",
		},
		expectErr: nil,
	}, {
		name: "valid lastModifier annotation label",
		annotations: map[string]string{
			"serving.knative.dev/lastModifier": "svc-modifier",
		},
		expectErr: nil,
	}, {
		name: "valid lastPinned annotation label",
		annotations: map[string]string{
			"serving.knative.dev/lastPinned": "pinned-val",
		},
		expectErr: nil,
	}, {
		name: "invalid knative prefix annotation",
		annotations: map[string]string{
			"serving.knative.dev/testAnnotation": "value",
		},
		expectErr: apis.ErrInvalidKeyName("serving.knative.dev/testAnnotation", ""),
	}, {
		name: "valid non-knative prefix annotation label",
		annotations: map[string]string{
			"testAnnotation": "testValue",
		},
		expectErr: nil,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateKnativeAnnotations(c.annotations)
			if d := cmp.Diff(c.expectErr.Error(), err.Error()); d != "" {
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
