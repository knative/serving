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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/apis/autoscaling"
	"github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	serving "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMakeMetric(t *testing.T) {
	cases := []struct {
		name   string
		pa     *v1alpha1.PodAutoscaler
		config *autoscaler.Config
		want   *autoscaler.Metric
	}{{
		name:   "defaults",
		pa:     pa(),
		config: config(),
		want:   metric(),
	}}

	for _, tc := range cases {
		if diff := cmp.Diff(tc.want, MakeMetric(context.TODO(), tc.pa, tc.config)); diff != "" {
			t.Errorf("%q (-want, +got):\n%v", tc.name, diff)
		}
	}
}

func pa(options ...PodAutoscalerOption) *v1alpha1.PodAutoscaler {
	p := &v1alpha1.PodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-namespace",
			Name:      "test-name",
			Annotations: map[string]string{
				autoscaling.ClassAnnotationKey: autoscaling.KPA,
			},
		},
		Spec: v1alpha1.PodAutoscalerSpec{
			ContainerConcurrency: serving.RevisionContainerConcurrencyType(0),
		},
		Status: v1alpha1.PodAutoscalerStatus{},
	}
	for _, fn := range options {
		fn(p)
	}
	return p
}

func config(options ...AutoscalerConfigOption) *autoscaler.Config {
	c := &autoscaler.Config{
		EnableScaleToZero:                    true,
		ContainerConcurrencyTargetPercentage: 1.0,
		ContainerConcurrencyTargetDefault:    100.0,
		MaxScaleUpRate:                       10.0,
		StableWindow:                         60 * time.Second,
		PanicWindow:                          6 * time.Second,
		TickInterval:                         2 * time.Second,
		ScaleToZeroGracePeriod:               30 * time.Second,
	}
	for _, fn := range options {
		fn(c)
	}
	return c
}

func metric(options ...MetricOption) *autoscaler.Metric {
	m := &autoscaler.Metric{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-namespace",
			Name:      "test-name",
			Annotations: map[string]string{
				autoscaling.ClassAnnotationKey: autoscaling.KPA,
			},
		},
		Spec: autoscaler.MetricSpec{
			TargetConcurrency: float64(100),
		},
	}
	for _, fn := range options {
		fn(m)
	}
	return m
}

type AutoscalerConfigOption func(*autoscaler.Config)

type MetricOption func(*autoscaler.Metric)

func withTarget1(metric *autoscaler.Metric) {
	metric.Spec.TargetConcurrency = 1
}
