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
	"context"
	"fmt"

	"github.com/knative/pkg/apis"
	"github.com/knative/serving/pkg/apis/autoscaling"
	"github.com/knative/serving/pkg/apis/serving"
	"k8s.io/apimachinery/pkg/api/equality"
)

func (pa *PodAutoscaler) Validate(ctx context.Context) *apis.FieldError {
	errs := serving.ValidateObjectMetadata(pa.GetObjectMeta()).ViaField("metadata")
	errs = errs.Also(pa.validateMetric())
	return errs.Also(pa.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec"))
}

// Validate validates PodAutoscaler Spec.
func (rs *PodAutoscalerSpec) Validate(ctx context.Context) *apis.FieldError {
	if equality.Semantic.DeepEqual(rs, &PodAutoscalerSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	errs := serving.ValidateNamespacedObjectReference(&rs.ScaleTargetRef).ViaField("scaleTargetRef")
	errs = errs.Also(rs.ContainerConcurrency.Validate(ctx).
		ViaField("containerConcurrency"))
	return errs.Also(validateSKSFields(ctx, rs))
}

func validateSKSFields(ctx context.Context, rs *PodAutoscalerSpec) *apis.FieldError {
	var all *apis.FieldError
	// TODO(vagababov) stop permitting empty protocol type, once SKS controller is live.
	if string(rs.ProtocolType) != "" {
		all = all.Also(rs.ProtocolType.Validate(ctx)).ViaField("protocolType")
	}
	return all
}

func (pa *PodAutoscaler) validateMetric() *apis.FieldError {
	if metric, ok := pa.Annotations[autoscaling.MetricAnnotationKey]; ok {
		switch pa.Class() {
		case autoscaling.KPA:
			switch metric {
			case autoscaling.Concurrency:
				return nil
			}
		case autoscaling.HPA:
			switch metric {
			case autoscaling.CPU, autoscaling.Concurrency:
				return nil
			}
			// TODO: implement OPS autoscaling.
		default:
			// Leave other classes of PodAutoscaler alone.
			return nil
		}
		return &apis.FieldError{
			Message: fmt.Sprintf("Unsupported metric %q for PodAutoscaler class %q",
				metric, pa.Class()),
			Paths: []string{"annotations[autoscaling.knative.dev/metric]"},
		}
	}
	return nil
}
