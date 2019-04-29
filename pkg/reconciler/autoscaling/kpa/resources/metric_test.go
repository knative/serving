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

package resources

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/apis/autoscaling"
	"github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	. "github.com/knative/serving/pkg/reconciler/testing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMakeMetric(t *testing.T) {
	cases := []struct {
		name string
		pa   *v1alpha1.PodAutoscaler
		want *autoscaler.Metric
	}{{
		name: "defaults",
		pa:   pa(),
		want: metric(),
	}, {
		name: "with longer stable window, no panic window percentage, defaults to 10%",
		pa:   pa(WithWindowAnnotation("10m")),
		want: metric(
			withStableWindow(10*time.Minute), withPanicWindow(time.Minute),
			withWindowAnnotation("10m")),
	}, {
		name: "with longer panic window percentage",
		pa:   pa(WithPanicWindowPercentageAnnotation("50")),
		want: metric(
			withStableWindow(time.Minute), withPanicWindow(30*time.Second),
			withPanicWindowPercentageAnnotation("50")),
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, MakeMetric(context.Background(), tc.pa, config)); diff != "" {
				t.Errorf("%q (-want, +got):\n%v", tc.name, diff)
			}
		})
	}
}

type MetricOption func(*autoscaler.Metric)

func withStableWindow(window time.Duration) MetricOption {
	return func(metric *autoscaler.Metric) {
		metric.Spec.StableWindow = window
	}
}

func withPanicWindow(window time.Duration) MetricOption {
	return func(metric *autoscaler.Metric) {
		metric.Spec.PanicWindow = window
	}
}

func withWindowAnnotation(window string) MetricOption {
	return func(metric *autoscaler.Metric) {
		metric.Annotations[autoscaling.WindowAnnotationKey] = window
	}
}

func withPanicWindowPercentageAnnotation(percentage string) MetricOption {
	return func(metric *autoscaler.Metric) {
		metric.Annotations[autoscaling.PanicWindowPercentageAnnotationKey] = percentage
	}
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
			StableWindow: 60 * time.Second,
			PanicWindow:  6 * time.Second,
		},
	}
	for _, fn := range options {
		fn(m)
	}
	return m
}
