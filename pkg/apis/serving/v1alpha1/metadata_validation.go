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
	"strconv"
	"strings"

	"github.com/knative/pkg/apis"
	"github.com/knative/serving/pkg/apis/autoscaling"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ValidateObjectMetadata(meta metav1.Object) *apis.FieldError {
	name := meta.GetName()

	if strings.Contains(name, ".") {
		return &apis.FieldError{
			Message: "Invalid resource name: special character . must not be present",
			Paths:   []string{"name"},
		}
	}

	if len(name) > 63 {
		return &apis.FieldError{
			Message: "Invalid resource name: length must be no more than 63 characters",
			Paths:   []string{"name"},
		}
	}

	if err := validateScaleBoundsAnnotations(meta.GetAnnotations()); err != nil {
		return err.ViaField("annotations")
	}

	return nil
}

func getIntGT0(m map[string]string, k string) (int64, *apis.FieldError) {
	v, ok := m[k]
	if ok {
		i, err := strconv.ParseInt(v, 10, 32)
		if err != nil || i < 1 {
			return 0, &apis.FieldError{
				Message: fmt.Sprintf("Invalid %s annotation value: must be integer greater than 0", k),
				Paths:   []string{k},
			}
		}
		return i, nil
	}
	return 0, nil
}

func validateScaleBoundsAnnotations(annotations map[string]string) *apis.FieldError {
	if annotations == nil {
		return nil
	}

	min, err := getIntGT0(annotations, autoscaling.MinScaleAnnotationKey)
	if err != nil {
		return err
	}
	max, err := getIntGT0(annotations, autoscaling.MaxScaleAnnotationKey)
	if err != nil {
		return err
	}

	if max != 0 && max < min {
		return &apis.FieldError{
			Message: fmt.Sprintf("%s=%v is less than %s=%v", autoscaling.MaxScaleAnnotationKey, max, autoscaling.MinScaleAnnotationKey, min),
			Paths:   []string{autoscaling.MaxScaleAnnotationKey, autoscaling.MinScaleAnnotationKey},
		}
	}

	return nil
}
