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
	"github.com/knative/pkg/apis"
	"github.com/knative/serving/pkg/apis/autoscaling"
	"reflect"
	"testing"
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
			Message: fmt.Sprintf("Invalid %s annotation value: must be integer greater than 0", autoscaling.MinScaleAnnotationKey),
			Paths:   []string{autoscaling.MinScaleAnnotationKey},
		},
	}, {
		name:        "maxScale is 0",
		annotations: map[string]string{autoscaling.MaxScaleAnnotationKey: "0"},
		expectErr: &apis.FieldError{
			Message: fmt.Sprintf("Invalid %s annotation value: must be integer greater than 0", autoscaling.MaxScaleAnnotationKey),
			Paths:   []string{autoscaling.MaxScaleAnnotationKey},
		},
	}, {
		name:        "minScale is foo",
		annotations: map[string]string{autoscaling.MinScaleAnnotationKey: "foo"},
		expectErr: &apis.FieldError{
			Message: fmt.Sprintf("Invalid %s annotation value: must be integer greater than 0", autoscaling.MinScaleAnnotationKey),
			Paths:   []string{autoscaling.MinScaleAnnotationKey},
		},
	}, {
		name:        "maxScale is bar",
		annotations: map[string]string{autoscaling.MaxScaleAnnotationKey: "bar"},
		expectErr: &apis.FieldError{
			Message: fmt.Sprintf("Invalid %s annotation value: must be integer greater than 0", autoscaling.MaxScaleAnnotationKey),
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
