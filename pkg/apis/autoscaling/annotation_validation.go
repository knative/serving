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
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
)

func getIntGE0(m map[string]string, k string) (int32, *apis.FieldError) {
	v, ok := m[k]
	if !ok {
		return 0, nil
	}
	// Parsing as uint gives a bad format error, rather than invalid range, unfortunately.
	i, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return 0, apis.ErrOutOfBoundsValue(v, 0, math.MaxInt32, k)
		}
		return 0, apis.ErrInvalidValue(v, k)
	}
	if i < 0 {
		return 0, apis.ErrOutOfBoundsValue(v, 0, math.MaxInt32, k)
	}

	return int32(i), nil
}

// ValidateAnnotations verifies the autoscaling annotations.
func ValidateAnnotations(ctx context.Context, config *autoscalerconfig.Config, anns map[string]string) *apis.FieldError {
	return validateClass(anns).
		Also(validateMinMaxScale(ctx, config, anns)).
		Also(validateFloats(anns)).
		Also(validateWindow(anns)).
		Also(validateLastPodRetention(anns)).
		Also(validateScaleDownDelay(anns)).
		Also(validateMetric(anns)).
		Also(validateInitialScale(config, anns))
}

func validateClass(annotations map[string]string) *apis.FieldError {
	if c, ok := annotations[ClassAnnotationKey]; ok {
		if strings.HasSuffix(c, domain) && c != KPA && c != HPA {
			return apis.ErrInvalidValue(c, ClassAnnotationKey)
		}
	}
	return nil
}

func validateFloats(annotations map[string]string) (errs *apis.FieldError) {
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

func validateScaleDownDelay(annotations map[string]string) *apis.FieldError {
	var errs *apis.FieldError
	if w, ok := annotations[ScaleDownDelayAnnotationKey]; ok {
		if d, err := time.ParseDuration(w); err != nil {
			errs = apis.ErrInvalidValue(w, ScaleDownDelayAnnotationKey)
		} else if d < 0 || d > WindowMax {
			// Since we disallow windows longer than WindowMax, so we should limit this
			// as well.
			errs = apis.ErrOutOfBoundsValue(w, 0*time.Second, WindowMax, ScaleDownDelayAnnotationKey)
		} else if d.Round(time.Second) != d {
			errs = apis.ErrGeneric("must be specified with at most second precision", ScaleDownDelayAnnotationKey)
		}
	}
	return errs
}

func validateLastPodRetention(annotations map[string]string) *apis.FieldError {
	if w, ok := annotations[ScaleToZeroPodRetentionPeriodKey]; ok {
		if d, err := time.ParseDuration(w); err != nil {
			return apis.ErrInvalidValue(w, ScaleToZeroPodRetentionPeriodKey)
		} else if d < 0 || d > WindowMax {
			// Since we disallow windows longer than WindowMax, so we should limit this
			// as well.
			return apis.ErrOutOfBoundsValue(w, time.Duration(0), WindowMax, ScaleToZeroPodRetentionPeriodKey)
		}
	}
	return nil
}

func validateWindow(annotations map[string]string) *apis.FieldError {
	if w, ok := annotations[WindowAnnotationKey]; ok {
		if annotations[ClassAnnotationKey] == HPA && annotations[MetricAnnotationKey] == CPU {
			return apis.ErrInvalidKeyName(WindowAnnotationKey, apis.CurrentField, fmt.Sprintf("%s for %s %s", HPA, MetricAnnotationKey, CPU))
		}
		switch d, err := time.ParseDuration(w); {
		case err != nil:
			return apis.ErrInvalidValue(w, WindowAnnotationKey)
		case d < WindowMin || d > WindowMax:
			return apis.ErrOutOfBoundsValue(w, WindowMin, WindowMax, WindowAnnotationKey)
		case d.Truncate(time.Second) != d:
			return apis.ErrGeneric("must be specified with at most second precision", WindowAnnotationKey)
		}
	}
	return nil
}

func validateMinMaxScale(ctx context.Context, config *autoscalerconfig.Config, annotations map[string]string) *apis.FieldError {
	min, errs := getIntGE0(annotations, MinScaleAnnotationKey)
	max, err := getIntGE0(annotations, MaxScaleAnnotationKey)
	errs = errs.Also(err)

	if max != 0 && max < min {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprintf("maxScale=%d is less than minScale=%d", max, min),
			Paths:   []string{MaxScaleAnnotationKey, MinScaleAnnotationKey},
		})
	}
	_, exists := annotations[MaxScaleAnnotationKey]
	// If max scale annotation is not set but default MaxScale is set, then max scale will not be unlimited.
	if !apis.IsInCreate(ctx) || config.MaxScaleLimit == 0 || (!exists && config.MaxScale > 0) {
		return errs
	}
	if max > config.MaxScaleLimit {
		errs = errs.Also(apis.ErrOutOfBoundsValue(
			max, 1, config.MaxScaleLimit, MaxScaleAnnotationKey))
	}
	if max == 0 {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprint("maxScale=0 (unlimited), must be less than ", config.MaxScaleLimit),
			Paths:   []string{MaxScaleAnnotationKey},
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

func validateInitialScale(config *autoscalerconfig.Config, annotations map[string]string) *apis.FieldError {
	if initialScale, ok := annotations[InitialScaleAnnotationKey]; ok {
		initScaleInt, err := strconv.Atoi(initialScale)
		if err != nil || initScaleInt < 0 || (!config.AllowZeroInitialScale && initScaleInt == 0) {
			return apis.ErrInvalidValue(initialScale, InitialScaleAnnotationKey)
		}
	}
	return nil
}
