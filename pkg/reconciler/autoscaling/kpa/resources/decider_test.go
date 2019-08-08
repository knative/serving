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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler"
	. "knative.dev/serving/pkg/testing"
)

func TestMakeDecider(t *testing.T) {
	cases := []struct {
		name   string
		pa     *v1alpha1.PodAutoscaler
		svc    string
		want   *autoscaler.Decider
		cfgOpt func(autoscaler.Config) *autoscaler.Config
	}{{
		name: "defaults",
		pa:   pa(),
		want: decider(withTarget(100.0), withPanicThreshold(200.0), withTotal(100)),
	}, {
		name: "tu < 1", // See #4449 why Target=100
		pa:   pa(),
		want: decider(withTarget(80), withPanicThreshold(160.0), withTotal(100)),
		cfgOpt: func(c autoscaler.Config) *autoscaler.Config {
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
	}, {
		name: "with container concurrency 1",
		pa:   pa(WithPAContainerConcurrency(1)),
		want: decider(withTarget(1.0), withPanicThreshold(2.0), withTotal(1)),
	}, {
		name: "with target annotation 1",
		pa:   pa(WithTargetAnnotation("1")),
		want: decider(withTarget(1.0), withTotal(1), withPanicThreshold(2.0), withTargetAnnotation("1")),
	}, {
		name: "with container concurrency and tu < 1",
		pa:   pa(WithPAContainerConcurrency(100)),
		want: decider(withTarget(80), withTotal(100), withPanicThreshold(160)), // PanicThreshold depends on TCC.
		cfgOpt: func(c autoscaler.Config) *autoscaler.Config {
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
	}, {
		name: "with burst capacity set",
		pa:   pa(WithPAContainerConcurrency(120)),
		want: decider(withTarget(96), withTotal(120), withPanicThreshold(192), withTargetBurstCapacity(63)),
		cfgOpt: func(c autoscaler.Config) *autoscaler.Config {
			c.TargetBurstCapacity = 63
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
	}, {
		name: "with burst capacity set on the annotation",
		pa:   pa(WithPAContainerConcurrency(120), withTBCAnnotation("211")),
		want: decider(withTarget(96), withTotal(120), withPanicThreshold(192),
			withDeciderTBCAnnotation("211"), withTargetBurstCapacity(211)),
		cfgOpt: func(c autoscaler.Config) *autoscaler.Config {
			c.TargetBurstCapacity = 63
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
	}, {
		name: "with container concurrency greater than target annotation (ok)",
		pa:   pa(WithPAContainerConcurrency(10), WithTargetAnnotation("1")),
		want: decider(withTarget(1.0), withTotal(1), withPanicThreshold(2.0), withTargetAnnotation("1")),
	}, {
		name: "with target annotation greater than container concurrency (ignore annotation for safety)",
		pa:   pa(WithPAContainerConcurrency(1), WithTargetAnnotation("10")),
		want: decider(withTarget(1.0), withTotal(1), withPanicThreshold(2.0), withTargetAnnotation("10")),
	}, {
		name: "with higher panic target",
		pa:   pa(WithTargetAnnotation("10"), WithPanicThresholdPercentageAnnotation("400")),
		want: decider(
			withTarget(10.0), withPanicThreshold(40.0), withTotal(10),
			withTargetAnnotation("10"), withPanicThresholdPercentageAnnotation("400")),
	}, {
		name: "with service name",
		pa:   pa(WithTargetAnnotation("10"), WithPanicThresholdPercentageAnnotation("400")),
		svc:  "rock-solid",
		want: decider(
			withService("rock-solid"),
			withTarget(10.0), withPanicThreshold(40.0), withTotal(10.0),
			withTargetAnnotation("10"), withPanicThresholdPercentageAnnotation("400")),
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config
			if tc.cfgOpt != nil {
				cfg = tc.cfgOpt(*config)
			}

			if diff := cmp.Diff(tc.want, MakeDecider(context.Background(), tc.pa, cfg, tc.svc)); diff != "" {
				t.Errorf("%q (-want, +got):\n%v", tc.name, diff)
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
				autoscaling.ClassAnnotationKey:  autoscaling.KPA,
				autoscaling.MetricAnnotationKey: autoscaling.Concurrency,
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

func withTBCAnnotation(tbc string) PodAutoscalerOption {
	return func(pa *v1alpha1.PodAutoscaler) {
		pa.Annotations[autoscaling.TargetBurstCapacityKey] = tbc
	}
}

func withDeciderTBCAnnotation(tbc string) DeciderOption {
	return func(d *autoscaler.Decider) {
		d.Annotations[autoscaling.TargetBurstCapacityKey] = tbc
	}
}

func decider(options ...DeciderOption) *autoscaler.Decider {
	m := &autoscaler.Decider{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-namespace",
			Name:      "test-name",
			Annotations: map[string]string{
				autoscaling.ClassAnnotationKey:  autoscaling.KPA,
				autoscaling.MetricAnnotationKey: autoscaling.Concurrency,
			},
		},
		Spec: autoscaler.DeciderSpec{
			MaxScaleUpRate:      config.MaxScaleUpRate,
			TickInterval:        config.TickInterval,
			TargetValue:         100,
			TotalValue:          100,
			TargetBurstCapacity: 211,
			PanicThreshold:      200,
			StableWindow:        config.StableWindow,
		},
	}
	for _, fn := range options {
		fn(m)
	}
	return m
}

type DeciderOption func(*autoscaler.Decider)

func withTargetBurstCapacity(tbc float64) DeciderOption {
	return func(decider *autoscaler.Decider) {
		decider.Spec.TargetBurstCapacity = tbc
	}
}

func withTotal(total float64) DeciderOption {
	return func(decider *autoscaler.Decider) {
		decider.Spec.TotalValue = total
	}
}

func withTarget(target float64) DeciderOption {
	return func(decider *autoscaler.Decider) {
		decider.Spec.TargetValue = target
	}
}

func withService(s string) DeciderOption {
	return func(d *autoscaler.Decider) {
		d.Spec.ServiceName = s
	}
}

func withPanicThreshold(threshold float64) DeciderOption {
	return func(decider *autoscaler.Decider) {
		decider.Spec.PanicThreshold = threshold
	}
}
func withTargetAnnotation(target string) DeciderOption {
	return func(decider *autoscaler.Decider) {
		decider.Annotations[autoscaling.TargetAnnotationKey] = target
	}
}

func withPanicThresholdPercentageAnnotation(percentage string) DeciderOption {
	return func(decider *autoscaler.Decider) {
		decider.Annotations[autoscaling.PanicThresholdPercentageAnnotationKey] = percentage
	}
}

var config = &autoscaler.Config{
	EnableScaleToZero:                  true,
	ContainerConcurrencyTargetFraction: 1.0,
	ContainerConcurrencyTargetDefault:  100.0,
	TargetBurstCapacity:                211.0,
	MaxScaleUpRate:                     10.0,
	StableWindow:                       60 * time.Second,
	PanicThresholdPercentage:           200,
	PanicWindow:                        6 * time.Second,
	PanicWindowPercentage:              10,
	TickInterval:                       2 * time.Second,
	ScaleToZeroGracePeriod:             30 * time.Second,
}
