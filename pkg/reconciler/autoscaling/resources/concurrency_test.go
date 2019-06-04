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
	"testing"
	"time"

	"github.com/knative/serving/pkg/apis/autoscaling"
	"github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	. "github.com/knative/serving/pkg/reconciler/testing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestResolveTargetConcurrency(t *testing.T) {
	cases := []struct {
		name string
		pa   *v1alpha1.PodAutoscaler
		want float64
	}{{
		name: "defaults",
		pa:   pa(),
		want: 100,
	}, {
		name: "with container concurrency 1",
		pa:   pa(WithContainerConcurrency(1)),
		want: 1.5,
	}, {
		name: "with target annotation 1",
		pa:   pa(WithTargetAnnotation("1")),
		want: 1,
	}, {
		name: "with container concurrency greater than target annotation (ok)",
		pa:   pa(WithContainerConcurrency(10), WithTargetAnnotation("1")),
		want: 1,
	}, {
		name: "with target annotation greater than container concurrency (ignore annotation for safety)",
		pa:   pa(WithContainerConcurrency(1), WithTargetAnnotation("10")),
		want: 1.5,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveTargetConcurrency(tc.pa, config); got != tc.want {
				t.Errorf("ResolveTargetConcurrency(%v, %v) = %v, want %v", tc.pa, config, got, tc.want)
			}
		})
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
			ContainerConcurrency: 0,
		},
		Status: v1alpha1.PodAutoscalerStatus{},
	}
	for _, fn := range options {
		fn(p)
	}
	return p
}

var config = &autoscaler.Config{
	EnableScaleToZero:                    true,
	ContainerConcurrencyTargetPercentage: 1.5,
	ContainerConcurrencyTargetDefault:    100.0,
	MaxScaleUpRate:                       10.0,
	StableWindow:                         60 * time.Second,
	PanicThresholdPercentage:             200,
	PanicWindow:                          6 * time.Second,
	PanicWindowPercentage:                10,
	TickInterval:                         2 * time.Second,
	ScaleToZeroGracePeriod:               30 * time.Second,
}
