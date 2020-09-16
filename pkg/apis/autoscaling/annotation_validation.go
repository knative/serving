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
	asconfig "knative.dev/serving/pkg/autoscaler/config"
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
	if c, ok := annotations[asconfig.ClassAnnotationKey]; ok {
		if strings.HasSuffix(c, domain) && c != asconfig.KPA && c != asconfig.HPA {
			return apis.ErrInvalidValue(c, asconfig.ClassAnnotationKey)
		}
	}
	return nil
}

func validateFloats(annotations map[string]string) *apis.FieldError {
	var errs *apis.FieldError
	if v, ok := annotations[asconfig.PanicWindowPercentageAnnotationKey]; ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil {
			errs = errs.Also(apis.ErrInvalidValue(v, asconfig.PanicWindowPercentageAnnotationKey))
		} else if fv < asconfig.PanicWindowPercentageMin || fv > asconfig.PanicWindowPercentageMax {
			errs = apis.ErrOutOfBoundsValue(v, asconfig.PanicWindowPercentageMin,
				asconfig.PanicWindowPercentageMax, asconfig.PanicWindowPercentageAnnotationKey)
		}
	}
	if v, ok := annotations[asconfig.PanicThresholdPercentageAnnotationKey]; ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil {
			errs = errs.Also(apis.ErrInvalidValue(v, asconfig.PanicThresholdPercentageAnnotationKey))
		} else if fv < asconfig.PanicThresholdPercentageMin || fv > asconfig.PanicThresholdPercentageMax {
			errs = errs.Also(apis.ErrOutOfBoundsValue(v, asconfig.PanicThresholdPercentageMin,
				asconfig.PanicThresholdPercentageMax, asconfig.PanicThresholdPercentageAnnotationKey))
		}
	}

	if v, ok := annotations[asconfig.TargetAnnotationKey]; ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil || fv < asconfig.TargetMin {
			errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("target %s should be at least %g", v, asconfig.TargetMin),
				asconfig.TargetAnnotationKey))
		}
	}

	if v, ok := annotations[asconfig.TargetUtilizationPercentageKey]; ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil {
			errs = errs.Also(apis.ErrInvalidValue(v, asconfig.TargetUtilizationPercentageKey))
		} else if fv < 1 || fv > 100 {
			errs = errs.Also(apis.ErrOutOfBoundsValue(v, 1, 100, asconfig.TargetUtilizationPercentageKey))
		}
	}

	if v, ok := annotations[asconfig.TargetBurstCapacityKey]; ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil || fv < 0 && fv != -1 {
			errs = errs.Also(apis.ErrInvalidValue(v, asconfig.TargetBurstCapacityKey))
		}
	}
	return errs
}

func validateLastPodRetention(annotations map[string]string) *apis.FieldError {
	var errs *apis.FieldError
	if w, ok := annotations[asconfig.ScaleToZeroPodRetentionPeriodKey]; ok {
		if d, err := time.ParseDuration(w); err != nil {
			errs = apis.ErrInvalidValue(w, asconfig.ScaleToZeroPodRetentionPeriodKey)
		} else if d < 0 || d > asconfig.WindowMax {
			// Since we disallow windows longer than WindowMax, so we should limit this
			// as well.
			errs = apis.ErrOutOfBoundsValue(w, 0*time.Second, asconfig.WindowMax, asconfig.ScaleToZeroPodRetentionPeriodKey)
		}
	}
	return errs
}

func validateWindow(annotations map[string]string) *apis.FieldError {
	var errs *apis.FieldError
	if w, ok := annotations[asconfig.WindowAnnotationKey]; ok {
		if annotations[asconfig.ClassAnnotationKey] == asconfig.HPA && annotations[asconfig.MetricAnnotationKey] == asconfig.CPU {
			return apis.ErrInvalidKeyName(asconfig.WindowAnnotationKey, apis.CurrentField,
				fmt.Sprintf("%s for %s %s", asconfig.HPA, asconfig.MetricAnnotationKey, asconfig.CPU))
		}
		if d, err := time.ParseDuration(w); err != nil {
			errs = apis.ErrInvalidValue(w, asconfig.WindowAnnotationKey)
		} else if d < asconfig.WindowMin || d > asconfig.WindowMax {
			errs = apis.ErrOutOfBoundsValue(w, asconfig.WindowMin, asconfig.WindowMax, asconfig.WindowAnnotationKey)
		}
	}
	return errs
}

func validateMinMaxScale(annotations map[string]string) *apis.FieldError {
	min, errs := getIntGE0(annotations, asconfig.MinScaleAnnotationKey)
	max, err := getIntGE0(annotations, asconfig.MaxScaleAnnotationKey)
	errs = errs.Also(err)

	if max != 0 && max < min {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprintf("maxScale=%d is less than minScale=%d", max, min),
			Paths:   []string{asconfig.MaxScaleAnnotationKey, asconfig.MinScaleAnnotationKey},
		})
	}
	return errs
}

func validateMetric(annotations map[string]string) *apis.FieldError {
	if metric, ok := annotations[asconfig.MetricAnnotationKey]; ok {
		classValue := asconfig.KPA
		if c, ok := annotations[asconfig.ClassAnnotationKey]; ok {
			classValue = c
		}
		switch classValue {
		case asconfig.KPA:
			switch metric {
			case asconfig.Concurrency, asconfig.RPS:
				return nil
			}
		case asconfig.HPA:
			switch metric {
			case asconfig.CPU:
				return nil
			}
		default:
			// Leave other classes of PodAutoscaler alone.
			return nil
		}
		return apis.ErrInvalidValue(metric, asconfig.MetricAnnotationKey)
	}
	return nil
}

func validateInitialScale(allowInitScaleZero bool, annotations map[string]string) *apis.FieldError {
	if initialScale, ok := annotations[asconfig.InitialScaleAnnotationKey]; ok {
		initScaleInt, err := strconv.Atoi(initialScale)
		if err != nil || initScaleInt < 0 || (!allowInitScaleZero && initScaleInt == 0) {
			return apis.ErrInvalidValue(initialScale, asconfig.InitialScaleAnnotationKey)
		}
	}
	return nil
}
