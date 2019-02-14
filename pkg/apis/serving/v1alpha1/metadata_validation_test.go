/*
Copyright 2018 The Knative Authors

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

package v1alpha1

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/knative/pkg/apis"
	"github.com/knative/serving/pkg/apis/autoscaling"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateScaleBoundAnnotations(t *testing.T) {
	cases := []struct {
		name        string
		annotations map[string]string
		expectErr   *apis.FieldError
	}{{
		name:        "nil annotations",
		annotations: nil,
		expectErr:   nil,
	}, {
		name:        "empty annotations",
		annotations: map[string]string{},
		expectErr:   nil,
	}, {
		name:        "minScale is 0",
		annotations: map[string]string{autoscaling.MinScaleAnnotationKey: "0"},
		expectErr: &apis.FieldError{
			Message: fmt.Sprintf("Invalid %s annotation value: must be an integer greater than 0", autoscaling.MinScaleAnnotationKey),
			Paths:   []string{autoscaling.MinScaleAnnotationKey},
		},
	}, {
		name:        "maxScale is 0",
		annotations: map[string]string{autoscaling.MaxScaleAnnotationKey: "0"},
		expectErr: &apis.FieldError{
			Message: fmt.Sprintf("Invalid %s annotation value: must be an integer greater than 0", autoscaling.MaxScaleAnnotationKey),
			Paths:   []string{autoscaling.MaxScaleAnnotationKey},
		},
	}, {
		name:        "minScale is foo",
		annotations: map[string]string{autoscaling.MinScaleAnnotationKey: "foo"},
		expectErr: &apis.FieldError{
			Message: fmt.Sprintf("Invalid %s annotation value: must be an integer greater than 0", autoscaling.MinScaleAnnotationKey),
			Paths:   []string{autoscaling.MinScaleAnnotationKey},
		},
	}, {
		name:        "maxScale is bar",
		annotations: map[string]string{autoscaling.MaxScaleAnnotationKey: "bar"},
		expectErr: &apis.FieldError{
			Message: fmt.Sprintf("Invalid %s annotation value: must be an integer greater than 0", autoscaling.MaxScaleAnnotationKey),
			Paths:   []string{autoscaling.MaxScaleAnnotationKey},
		},
	}, {
		name:        "minScale is 5",
		annotations: map[string]string{autoscaling.MinScaleAnnotationKey: "5"},
		expectErr:   nil,
	}, {
		name:        "maxScale is 2",
		annotations: map[string]string{autoscaling.MaxScaleAnnotationKey: "2"},
		expectErr:   nil,
	}, {
		name:        "minScale is 2, maxScale is 5",
		annotations: map[string]string{autoscaling.MinScaleAnnotationKey: "2", autoscaling.MaxScaleAnnotationKey: "5"},
		expectErr:   nil,
	}, {
		name:        "minScale is 5, maxScale is 2",
		annotations: map[string]string{autoscaling.MinScaleAnnotationKey: "5", autoscaling.MaxScaleAnnotationKey: "2"},
		expectErr: &apis.FieldError{
			Message: fmt.Sprintf("%s=%v is less than %s=%v", autoscaling.MaxScaleAnnotationKey, 2, autoscaling.MinScaleAnnotationKey, 5),
			Paths:   []string{autoscaling.MaxScaleAnnotationKey, autoscaling.MinScaleAnnotationKey},
		},
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateScaleBoundsAnnotations(c.annotations)
			if !reflect.DeepEqual(c.expectErr, err) {
				t.Errorf("Expected: '%+v', Got: '%+v'", c.expectErr, err)
			}
		})
	}
}

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
