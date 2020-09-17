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
	"math"
	"strconv"
	"strings"
	"time"

	"knative.dev/pkg/apis"
)

func getIntGE0(m map[string]string, k string) (int64, *apis.FieldError) {
	v, ok := m[k]
	if !ok {
		return 0, nil
	}
	i, err := strconv.ParseInt(v, 10, 32)
	if err == nil && i < 0 {
		return 0, apis.ErrOutOfBoundsValue(v, 0, math.MaxInt32, k)
	}
	if err != nil {
		if nerr, ok := err.(*strconv.NumError); ok && nerr.Err == strconv.ErrRange {
			return 0, apis.ErrOutOfBoundsValue(v, 0, math.MaxInt32, k)
		}
		return 0, apis.ErrInvalidValue(v, k)
	}

	return i, nil
}

// ValidateAnnotations verifies the autoscaling annotations.
func ValidateAnnotations(allowInitScaleZero bool, anns map[string]string) *apis.FieldError {
	if len(anns) == 0 {
		return nil
	}
	return validateClass(anns).Also(validateMinMaxScale(anns)).Also(validateFloats(anns)).
		Also(validateWindow(anns).Also(validateLastPodRetention(anns)).
			Also(validateMetric(anns).Also(validateInitialScale(allowInitScaleZero, anns))))
}

func validateClass(annotations map[string]string) *apis.FieldError {
	if c, ok := annotations[ClassAnnotationKey]; ok {
		if strings.HasSuffix(c, domain) && c != KPA && c != HPA {
			return apis.ErrInvalidValue(c, ClassAnnotationKey)
		}
	}
	return nil
}

func validateFloats(annotations map[string]string) *apis.FieldError {
	var errs *apis.FieldError
	if v, ok := annotations[PanicWindowPercentageAnnotationKey]; ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil {
			errs = errs.Also(apis.ErrInvalidValue(v, PanicWindowPercentageAnnotationKey))
		} else if fv < PanicWindowPercentageMin || fv > PanicWindowPercentageMax {
			errs = apis.ErrOutOfBoundsValue(v, PanicWindowPercentageMin,
				PanicWindowPercentageMax, PanicWindowPercentageAnnotationKey)
		}
	}
	if v, ok := annotations[PanicThresholdPercentageAnnotationKey]; ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil {
			errs = errs.Also(apis.ErrInvalidValue(v, PanicThresholdPercentageAnnotationKey))
		} else if fv < PanicThresholdPercentageMin || fv > PanicThresholdPercentageMax {
			errs = errs.Also(apis.ErrOutOfBoundsValue(v, PanicThresholdPercentageMin,
				PanicThresholdPercentageMax, PanicThresholdPercentageAnnotationKey))
		}
	}

	if v, ok := annotations[TargetAnnotationKey]; ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil || fv < TargetMin {
			errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("target %s should be at least %g", v, TargetMin), TargetAnnotationKey))
		}
	}

	if v, ok := annotations[TargetUtilizationPercentageKey]; ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil {
			errs = errs.Also(apis.ErrInvalidValue(v, TargetUtilizationPercentageKey))
		} else if fv < 1 || fv > 100 {
			errs = errs.Also(apis.ErrOutOfBoundsValue(v, 1, 100, TargetUtilizationPercentageKey))
		}
	}

	if v, ok := annotations[TargetBurstCapacityKey]; ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil || fv < 0 && fv != -1 {
			errs = errs.Also(apis.ErrInvalidValue(v, TargetBurstCapacityKey))
		}
	}
	return errs
}

func validateLastPodRetention(annotations map[string]string) *apis.FieldError {
	var errs *apis.FieldError
	if w, ok := annotations[ScaleToZeroPodRetentionPeriodKey]; ok {
		if d, err := time.ParseDuration(w); err != nil {
			errs = apis.ErrInvalidValue(w, ScaleToZeroPodRetentionPeriodKey)
		} else if d < 0 || d > WindowMax {
			// Since we disallow windows longer than WindowMax, so we should limit this
			// as well.
			errs = apis.ErrOutOfBoundsValue(w, 0*time.Second, WindowMax, ScaleToZeroPodRetentionPeriodKey)
		}
	}
	return errs
}

func validateWindow(annotations map[string]string) *apis.FieldError {
	var errs *apis.FieldError
	if w, ok := annotations[WindowAnnotationKey]; ok {
		if annotations[ClassAnnotationKey] == HPA && annotations[MetricAnnotationKey] == CPU {
			return apis.ErrInvalidKeyName(WindowAnnotationKey, apis.CurrentField, fmt.Sprintf("%s for %s %s", HPA, MetricAnnotationKey, CPU))
		}
		if d, err := time.ParseDuration(w); err != nil {
			errs = apis.ErrInvalidValue(w, WindowAnnotationKey)
		} else if d < WindowMin || d > WindowMax {
			errs = apis.ErrOutOfBoundsValue(w, WindowMin, WindowMax, WindowAnnotationKey)
		}
	}
	return errs
}

func validateMinMaxScale(annotations map[string]string) *apis.FieldError {
	min, errs := getIntGE0(annotations, MinScaleAnnotationKey)
	max, err := getIntGE0(annotations, MaxScaleAnnotationKey)
	errs = errs.Also(err)

	if max != 0 && max < min {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprintf("maxScale=%d is less than minScale=%d", max, min),
			Paths:   []string{MaxScaleAnnotationKey, MinScaleAnnotationKey},
		})
	}
	return errs
}

func validateMetric(annotations map[string]string) *apis.FieldError {
	if metric, ok := annotations[MetricAnnotationKey]; ok {
		classValue := KPA
		if c, ok := annotations[ClassAnnotationKey]; ok {
			classValue = c
		}
		switch classValue {
		case KPA:
			switch metric {
			case Concurrency, RPS:
				return nil
			}
		case HPA:
			switch metric {
			case CPU:
				return nil
			}
		default:
			// Leave other classes of PodAutoscaler alone.
			return nil
		}
		return apis.ErrInvalidValue(metric, MetricAnnotationKey)
	}
	return nil
}

func validateInitialScale(allowInitScaleZero bool, annotations map[string]string) *apis.FieldError {
	if initialScale, ok := annotations[InitialScaleAnnotationKey]; ok {
		initScaleInt, err := strconv.Atoi(initialScale)
		if err != nil || initScaleInt < 0 || (!allowInitScaleZero && initScaleInt == 0) {
			return apis.ErrInvalidValue(initialScale, InitialScaleAnnotationKey)
		}
	}
	return nil
}
