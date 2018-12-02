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

package resources

import (
	"context"

	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
)

func MakeMetric(ctx context.Context, pa *v1alpha1.PodAutoscaler, config *autoscaler.Config) *autoscaler.Metric {
	logger := logging.FromContext(ctx)

	target := config.TargetConcurrency(pa.Spec.ContainerConcurrency)
	if mt, ok := pa.MetricTarget(); ok {
		customTarget := float64(mt)
		if customTarget < target {
			logger.Infof("Ignoring target of %v because it would underprovision the Revision.", customTarget)
		} else {
			logger.Debugf("Using target of %v", customTarget)
			target = customTarget
		}
	}
	return &autoscaler.Metric{
		ObjectMeta: pa.ObjectMeta,
		Spec: autoscaler.MetricSpec{
			TargetConcurrency: target,
		},
	}
}
