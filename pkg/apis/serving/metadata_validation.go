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
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/config"
)

// ValidateObjectMetadata validates that the `metadata` stanza of the
// resources is correct.
// If `allowAutoscalingAnnotations` is true autoscaling annotations, if
// present, are validated. If `allowAutoscalingAnnotations` is false
// autoscaling annotations are validated not to be present.
func ValidateObjectMetadata(ctx context.Context, meta metav1.Object, allowAutoscalingAnnotations bool) *apis.FieldError {
	errs := apis.ValidateObjectMetadata(meta)

	if allowAutoscalingAnnotations {
		errs = errs.Also(autoscaling.ValidateAnnotations(ctx, config.FromContextOrDefaults(ctx).Autoscaler, meta.GetAnnotations()).ViaField("annotations"))
	} else {
		errs = errs.Also(ValidateHasNoAutoscalingAnnotation(meta.GetAnnotations()).ViaField("annotations"))
	}

	return errs
}

// ValidateRolloutDurationAnnotation validates the rollout duration annotation.
// This annotation can be set on either service or route objects.
func ValidateRolloutDurationAnnotation(annos map[string]string) (errs *apis.FieldError) {
	if k, v, _ := RolloutDurationAnnotation.Get(annos); v != "" {
		// Parse as duration.
		d, err := time.ParseDuration(v)
		if err != nil {
			return errs.Also(apis.ErrInvalidValue(v, k))
		}
		// Validate that it has second precision.
		if d.Round(time.Second) != d {
			return errs.Also(&apis.FieldError{
				// Even if tempting %v won't work here, since it might output the value spelled differently.
				Message: fmt.Sprintf("rollout-duration=%s is not at second precision", v),
				Paths:   []string{k},
			})
		}
		// And positive.
		if d < 0 {
			return errs.Also(&apis.FieldError{
				Message: fmt.Sprintf("rollout-duration=%s must be positive", v),
				Paths:   []string{k},
			})
		}
	}
	return errs
}

// ValidateHasNoAutoscalingAnnotation validates that the respective entity does not have
// annotations from the autoscaling group. It's to be used to validate Service and
// Configuration.
func ValidateHasNoAutoscalingAnnotation(annotations map[string]string) (errs *apis.FieldError) {
	for key := range annotations {
		if strings.HasPrefix(key, autoscaling.GroupName) {
			errs = errs.Also(
				apis.ErrInvalidKeyName(key, apis.CurrentField, `autoscaling annotations must be put under "spec.template.metadata.annotations" to work`))
		}
	}
	return errs
}

// ValidateContainerConcurrency function validates the ContainerConcurrency field
// TODO(#5007): Move this to autoscaling.
func ValidateContainerConcurrency(ctx context.Context, containerConcurrency *int64) *apis.FieldError {
	if containerConcurrency != nil {
		cfg := config.FromContextOrDefaults(ctx).Defaults

		var minContainerConcurrency int64
		if !cfg.AllowContainerConcurrencyZero {
			minContainerConcurrency = 1
		}

		if *containerConcurrency < minContainerConcurrency || *containerConcurrency > cfg.ContainerConcurrencyMaxLimit {
			return apis.ErrOutOfBoundsValue(
				*containerConcurrency, minContainerConcurrency, cfg.ContainerConcurrencyMaxLimit, apis.CurrentField)
		}
	}
	return nil
}

// SetUserInfo sets creator and updater annotations
func SetUserInfo(ctx context.Context, oldSpec, newSpec, resource interface{}) {
	if ui := apis.GetUserInfo(ctx); ui != nil {
		objectMetaAccessor, ok := resource.(metav1.ObjectMetaAccessor)
		if !ok {
			return
		}
		ans := objectMetaAccessor.GetObjectMeta().GetAnnotations()
		if ans == nil {
			ans = map[string]string{}
			objectMetaAccessor.GetObjectMeta().SetAnnotations(ans)
		}

		if apis.IsInUpdate(ctx) {
			if equality.Semantic.DeepEqual(oldSpec, newSpec) {
				return
			}
			ans[UpdaterAnnotation] = ui.Username
		} else {
			ans[CreatorAnnotation] = ui.Username
			ans[UpdaterAnnotation] = ui.Username
		}
	}
}
