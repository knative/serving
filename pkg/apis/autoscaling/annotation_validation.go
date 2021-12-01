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
	"knative.dev/pkg/kmap"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
)

func getIntGE0(m map[string]string, key kmap.KeyPriority) (int32, *apis.FieldError) {
	k, v, ok := key.Get(m)
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
		Also(validateMinMaxScale(config, anns)).
		Also(validateFloats(anns)).
		Also(validateWindow(anns)).
		Also(validateLastPodRetention(anns)).
		Also(validateScaleDownDelay(anns)).
		Also(validateMetric(anns)).
		Also(validateAlgorithm(anns)).
		Also(validateInitialScale(config, anns))
}

func validateClass(m map[string]string) *apis.FieldError {
	if k, v, ok := ClassAnnotation.Get(m); ok {
		if strings.HasSuffix(v, domain) && v != KPA && v != HPA {
			return apis.ErrInvalidValue(v, k)
		}
	}
	return nil
}

func validateAlgorithm(m map[string]string) *apis.FieldError {
	// Not a KPA? Don't validate, custom autoscalers might have custom values.
	if _, v, _ := ClassAnnotation.Get(m); v != KPA {
		return nil
	}
	if k, v, _ := MetricAggregationAlgorithmAnnotation.Get(m); v != "" {
		switch v {
		case MetricAggregationAlgorithmLinear,
			MetricAggregationAlgorithmWeightedExponential,
			MetricAggregationAlgorithmWeightedExponentialAlt:
			return nil
		default:
			return apis.ErrInvalidValue(v, k)
		}
	}
	return nil
}

func validateFloats(m map[string]string) (errs *apis.FieldError) {
	if k, v, ok := PanicWindowPercentageAnnotation.Get(m); ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil {
			errs = errs.Also(apis.ErrInvalidValue(v, k))
		} else if fv < PanicWindowPercentageMin || fv > PanicWindowPercentageMax {
			errs = apis.ErrOutOfBoundsValue(v, PanicWindowPercentageMin,
				PanicWindowPercentageMax, k)
		}
	}
	if k, v, ok := PanicThresholdPercentageAnnotation.Get(m); ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil {
			errs = errs.Also(apis.ErrInvalidValue(v, k))
		} else if fv < PanicThresholdPercentageMin || fv > PanicThresholdPercentageMax {
			errs = errs.Also(apis.ErrOutOfBoundsValue(v, PanicThresholdPercentageMin,
				PanicThresholdPercentageMax, k))
		}
	}

	if k, v, ok := TargetAnnotation.Get(m); ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil || fv < TargetMin {
			errs = errs.Also(apis.ErrGeneric(fmt.Sprintf("target %s should be at least %g", v, TargetMin), k))
		}
	}

	if k, v, ok := TargetUtilizationPercentageAnnotation.Get(m); ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil {
			errs = errs.Also(apis.ErrInvalidValue(v, k))
		} else if fv < 1 || fv > 100 {
			errs = errs.Also(apis.ErrOutOfBoundsValue(v, 1, 100, k))
		}
	}

	if k, v, ok := TargetBurstCapacityAnnotation.Get(m); ok {
		if fv, err := strconv.ParseFloat(v, 64); err != nil || fv < 0 && fv != -1 {
			errs = errs.Also(apis.ErrInvalidValue(v, k))
		}
	}
	return errs
}

func validateScaleDownDelay(m map[string]string) *apis.FieldError {
	var errs *apis.FieldError
	if k, v, ok := ScaleDownDelayAnnotation.Get(m); ok {
		if d, err := time.ParseDuration(v); err != nil {
			errs = apis.ErrInvalidValue(v, k)
		} else if d < 0 || d > WindowMax {
			// Since we disallow windows longer than WindowMax, so we should limit this
			// as well.
			errs = apis.ErrOutOfBoundsValue(v, 0*time.Second, WindowMax, k)
		} else if d.Round(time.Second) != d {
			errs = apis.ErrGeneric("must be specified with at most second precision", k)
		}
	}
	return errs
}

func validateLastPodRetention(m map[string]string) *apis.FieldError {
	if k, v, ok := ScaleToZeroPodRetentionPeriodAnnotation.Get(m); ok {
		if d, err := time.ParseDuration(v); err != nil {
			return apis.ErrInvalidValue(v, k)
		} else if d < 0 || d > WindowMax {
			// Since we disallow windows longer than WindowMax, so we should limit this
			// as well.
			return apis.ErrOutOfBoundsValue(v, time.Duration(0), WindowMax, k)
		}
	}
	return nil
}

func validateWindow(m map[string]string) *apis.FieldError {
	if _, v, ok := WindowAnnotation.Get(m); ok {
		switch d, err := time.ParseDuration(v); {
		case err != nil:
			return apis.ErrInvalidValue(v, WindowAnnotationKey)
		case d < WindowMin || d > WindowMax:
			return apis.ErrOutOfBoundsValue(v, WindowMin, WindowMax, WindowAnnotationKey)
		case d.Truncate(time.Second) != d:
			return apis.ErrGeneric("must be specified with at most second precision", WindowAnnotationKey)
		}
	}
	return nil
}

func validateMinMaxScale(config *autoscalerconfig.Config, m map[string]string) *apis.FieldError {
	min, errs := getIntGE0(m, MinScaleAnnotation)
	max, err := getIntGE0(m, MaxScaleAnnotation)
	errs = errs.Also(err)

	if max != 0 && max < min {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprintf("max-scale=%d is less than min-scale=%d", max, min),
			Paths:   []string{MaxScaleAnnotationKey, MinScaleAnnotationKey},
		})
	}

	if k, _, ok := MaxScaleAnnotation.Get(m); ok {
		errs = errs.Also(validateMaxScaleWithinLimit(k, max, config.MaxScaleLimit))
	}

	return errs
}

func validateMaxScaleWithinLimit(key string, maxScale, maxScaleLimit int32) (errs *apis.FieldError) {
	if maxScaleLimit == 0 {
		return nil
	}

	if maxScale > maxScaleLimit {
		errs = errs.Also(apis.ErrOutOfBoundsValue(maxScale, 1, maxScaleLimit, key))
	}

	if maxScale == 0 {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprint("max-scale=0 (unlimited), must be less than ", maxScaleLimit),
			Paths:   []string{key},
		})
	}

	return errs
}

func validateMetric(m map[string]string) *apis.FieldError {
	if _, metric, ok := MetricAnnotation.Get(m); ok {
		classValue := KPA
		if _, c, ok := ClassAnnotation.Get(m); ok {
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
			case "":
				break
			default:
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

func validateInitialScale(config *autoscalerconfig.Config, m map[string]string) *apis.FieldError {
	if k, v, ok := InitialScaleAnnotation.Get(m); ok {
		initScaleInt, err := strconv.Atoi(v)
		if err != nil || initScaleInt < 0 || (!config.AllowZeroInitialScale && initScaleInt == 0) {
			return apis.ErrInvalidValue(v, k)
		}
	}
	return nil
}
