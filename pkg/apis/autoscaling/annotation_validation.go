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

package autoscaling

import (
	"fmt"
	"strconv"

	"knative.dev/pkg/apis"
)

func getIntGE0(m map[string]string, k string) (int64, *apis.FieldError) {
	v, ok := m[k]
	if !ok {
		return 0, nil
	}
	i, err := strconv.ParseInt(v, 10, 32)
	if err != nil || i < 0 {
		return 0, &apis.FieldError{
			Message: "invalid value: must be an integer equal or greater than 0",
			Paths:   []string{annotationKey(k)},
		}
	}
	return i, nil
}

func ValidateAnnotations(annotations map[string]string) *apis.FieldError {
	if len(annotations) == 0 {
		return nil
	}

	return validateMinMaxScale(annotations).Also(validateWindows(annotations))
}

func validateWindows(annotations map[string]string) *apis.FieldError {
	var errs *apis.FieldError
	if v, ok := annotations[PanicWindowPercentageAnnotationKey]; ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil {
			errs = errs.Also(apis.ErrInvalidValue(v, annotationKey(PanicWindowPercentageAnnotationKey)))
		} else if fv < PanicWindowPercentageMin || fv > PanicWindowPercentageMax {
			errs = apis.ErrOutOfBoundsValue(v, PanicWindowPercentageMin,
				PanicWindowPercentageMax, annotationKey(PanicWindowPercentageAnnotationKey))
		}
	}
	if v, ok := annotations[PanicThresholdPercentageAnnotationKey]; ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil {
			errs = errs.Also(apis.ErrInvalidValue(v, annotationKey(PanicThresholdPercentageAnnotationKey)))
		} else if fv < PanicThresholdPercentageMin {
			errs = errs.Also(&apis.FieldError{
				Message: fmt.Sprintf("invalid value %s, must be at least %v", v, PanicThresholdPercentageMin),
				Paths:   []string{annotationKey(PanicThresholdPercentageAnnotationKey)},
			})
		}
	}
	return errs
}

func validateMinMaxScale(annotations map[string]string) *apis.FieldError {
	var errs *apis.FieldError

	min, err := getIntGE0(annotations, MinScaleAnnotationKey)
	if err != nil {
		errs = err
	}
	max, err := getIntGE0(annotations, MaxScaleAnnotationKey)
	if err != nil {
		errs = errs.Also(err)
	}

	if max != 0 && max < min {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprintf("%s=%v is less than %s=%v", MaxScaleAnnotationKey, max, MinScaleAnnotationKey, min),
			Paths:   []string{annotationKey(MaxScaleAnnotationKey), annotationKey(MinScaleAnnotationKey)},
		})
	}
	return errs
}

func annotationKey(ak string) string {
	return "annotation: " + ak
}
