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
	asconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
	"knative.dev/serving/pkg/autoscaler/scaling"
	. "knative.dev/serving/pkg/testing"
)

func TestMakeDecider(t *testing.T) {
	cases := []struct {
		name   string
		pa     *v1alpha1.PodAutoscaler
		want   *scaling.Decider
		cfgOpt func(autoscalerconfig.Config) *autoscalerconfig.Config
	}{{
		name: "defaults",
		pa:   pa(),
		want: decider(withTarget(100.0), withPanicThreshold(2.0), withTotal(100)),
	}, {
		name: "unreachable",
		pa: pa(func(pa *v1alpha1.PodAutoscaler) {
			pa.Spec.Reachability = v1alpha1.ReachabilityUnreachable
		}),
		want: decider(withTarget(100.0), withPanicThreshold(2.0), withTotal(100), func(d *scaling.Decider) {
			d.Spec.Reachable = false
		}),
	}, {
		name: "explicit reachable",
		pa: pa(func(pa *v1alpha1.PodAutoscaler) {
			pa.Spec.Reachability = v1alpha1.ReachabilityReachable
		}),
		want: decider(withTarget(100.0), withPanicThreshold(2.0), withTotal(100)),
	}, {
		name: "tu < 1", // See #4449 why Target=100
		pa:   pa(),
		want: decider(withTarget(80), withPanicThreshold(2.0), withTotal(100)),
		cfgOpt: func(c autoscalerconfig.Config) *autoscalerconfig.Config {
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
	}, {
		name: "scale up and scale down rates",
		pa:   pa(),
		want: decider(withTarget(100.0), withPanicThreshold(2.0), withTotal(100),
			withScaleUpDownRates(19.84, 19.88)),
		cfgOpt: func(c autoscalerconfig.Config) *autoscalerconfig.Config {
			c.MaxScaleUpRate = 19.84
			c.MaxScaleDownRate = 19.88
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
		want: decider(withTarget(80), withTotal(100), withPanicThreshold(2.0)),
		cfgOpt: func(c autoscalerconfig.Config) *autoscalerconfig.Config {
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
	}, {
		name: "with burst capacity set",
		pa:   pa(WithPAContainerConcurrency(120)),
		want: decider(withTarget(96), withTotal(120), withPanicThreshold(2.0), withTargetBurstCapacity(63)),
		cfgOpt: func(c autoscalerconfig.Config) *autoscalerconfig.Config {
			c.TargetBurstCapacity = 63
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
	}, {
		name: "with activator capacity override",
		pa:   pa(),
		want: decider(withActivatorCapacity(420), withTarget(100.0), withPanicThreshold(2.0), withTotal(100)),
		cfgOpt: func(c autoscalerconfig.Config) *autoscalerconfig.Config {
			c.ActivatorCapacity = 420
			return &c
		},
	}, {
		name: "with burst capacity set on the annotation",
		pa:   pa(WithPAContainerConcurrency(120), withTBCAnnotation("211")),
		want: decider(withTarget(96), withTotal(120), withPanicThreshold(2.0),
			withDeciderTBCAnnotation("211"), withTargetBurstCapacity(211)),
		cfgOpt: func(c autoscalerconfig.Config) *autoscalerconfig.Config {
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
			withTarget(10.0), withPanicThreshold(4.0), withTotal(10),
			withTargetAnnotation("10"), withPanicThresholdPercentageAnnotation("400")),
	}, {
		name: "with metric annotation",
		pa:   pa(WithMetricAnnotation("rps")),
		want: decider(withTarget(100.0), withPanicThreshold(2.0), withTotal(100), withMetric("rps"), withMetricAnnotation("rps")),
	}, {
		name: "with initial scale",
		pa: pa(func(pa *v1alpha1.PodAutoscaler) {
			pa.Annotations[autoscaling.InitialScaleAnnotationKey] = "2"
		}),
		want: decider(withTarget(100.0), withPanicThreshold(2.0), withTotal(100),
			func(d *scaling.Decider) {
				d.Spec.InitialScale = 2
				d.Annotations[autoscaling.InitialScaleAnnotationKey] = "2"
			}),
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config
			if tc.cfgOpt != nil {
				cfg = tc.cfgOpt(*config)
			}

			if diff := cmp.Diff(tc.want, MakeDecider(context.Background(), tc.pa, cfg)); diff != "" {
				t.Errorf("%q (-want, +got):\n%v", tc.name, diff)
			}
		})
	}
}

func TestGetInitialScale(t *testing.T) {
	tests := []struct {
		name          string
		paMutation    func(*v1alpha1.PodAutoscaler)
		configMutator func(*autoscalerconfig.Config)
		want          int
	}{{
		name: "revision initial scale not set",
		want: 1,
	}, {
		name: "revision initial scale overrides cluster initial scale",
		paMutation: func(pa *v1alpha1.PodAutoscaler) {
			pa.Annotations[autoscaling.InitialScaleAnnotationKey] = "2"
		},
		want: 2,
	}, {
		name: "cluster allows initial scale zero",
		paMutation: func(pa *v1alpha1.PodAutoscaler) {
			pa.Annotations[autoscaling.InitialScaleAnnotationKey] = "0"
		},
		configMutator: func(c *autoscalerconfig.Config) {
			c.AllowZeroInitialScale = true
		},
		want: 0,
	}, {
		name: "cluster does not allows initial scale zero",
		paMutation: func(pa *v1alpha1.PodAutoscaler) {
			pa.Annotations[autoscaling.InitialScaleAnnotationKey] = "0"
		},
		configMutator: func(c *autoscalerconfig.Config) {
			c.AllowZeroInitialScale = false
		},
		want: 1,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ac, _ := asconfig.NewConfigFromMap(map[string]string{})
			if test.configMutator != nil {
				test.configMutator(ac)
			}
			pa := pa()
			if test.paMutation != nil {
				test.paMutation(pa)
			}
			got := int(GetInitialScale(ac, pa))
			if want := test.want; got != want {
				t.Errorf("got = %v, want: %v", got, want)
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

func withDeciderTBCAnnotation(tbc string) deciderOption {
	return func(d *scaling.Decider) {
		d.Annotations[autoscaling.TargetBurstCapacityKey] = tbc
	}
}

type deciderOption func(*scaling.Decider)

func decider(options ...deciderOption) *scaling.Decider {
	m := &scaling.Decider{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-namespace",
			Name:      "test-name",
			Annotations: map[string]string{
				autoscaling.ClassAnnotationKey:  autoscaling.KPA,
				autoscaling.MetricAnnotationKey: autoscaling.Concurrency,
			},
		},
		Spec: scaling.DeciderSpec{
			MaxScaleUpRate:      config.MaxScaleUpRate,
			ScalingMetric:       "concurrency",
			TargetValue:         100,
			TotalValue:          100,
			TargetBurstCapacity: 211,
			PanicThreshold:      200,
			ActivatorCapacity:   811,
			StableWindow:        config.StableWindow,
			InitialScale:        1,
			Reachable:           true,
		},
	}
	for _, fn := range options {
		fn(m)
	}
	return m
}

func withActivatorCapacity(x float64) deciderOption {
	return func(d *scaling.Decider) {
		d.Spec.ActivatorCapacity = x
	}
}

func withMetric(metric string) deciderOption {
	return func(decider *scaling.Decider) {
		decider.Spec.ScalingMetric = metric
	}
}

func withTargetBurstCapacity(tbc float64) deciderOption {
	return func(decider *scaling.Decider) {
		decider.Spec.TargetBurstCapacity = tbc
	}
}

func withScaleUpDownRates(up, down float64) deciderOption {
	return func(decider *scaling.Decider) {
		decider.Spec.MaxScaleUpRate = up
		decider.Spec.MaxScaleDownRate = down
	}
}

func withTotal(total float64) deciderOption {
	return func(decider *scaling.Decider) {
		decider.Spec.TotalValue = total
	}
}

func withTarget(target float64) deciderOption {
	return func(decider *scaling.Decider) {
		decider.Spec.TargetValue = target
	}
}

func withPanicThreshold(threshold float64) deciderOption {
	return func(decider *scaling.Decider) {
		decider.Spec.PanicThreshold = threshold
	}
}

func withTargetAnnotation(target string) deciderOption {
	return func(decider *scaling.Decider) {
		decider.Annotations[autoscaling.TargetAnnotationKey] = target
	}
}

func withMetricAnnotation(metric string) deciderOption {
	return func(decider *scaling.Decider) {
		decider.Annotations[autoscaling.MetricAnnotationKey] = metric
	}
}

func withPanicThresholdPercentageAnnotation(percentage string) deciderOption {
	return func(decider *scaling.Decider) {
		decider.Annotations[autoscaling.PanicThresholdPercentageAnnotationKey] = percentage
	}
}

var config = &autoscalerconfig.Config{
	EnableScaleToZero:                  true,
	ContainerConcurrencyTargetFraction: 1.0,
	ContainerConcurrencyTargetDefault:  100.0,
	TargetBurstCapacity:                211.0,
	MaxScaleUpRate:                     10.0,
	RPSTargetDefault:                   100,
	ActivatorCapacity:                  811,
	TargetUtilization:                  1.0,
	StableWindow:                       60 * time.Second,
	PanicThresholdPercentage:           200,
	PanicWindowPercentage:              10,
	ScaleToZeroGracePeriod:             30 * time.Second,
	InitialScale:                       1,
	AllowZeroInitialScale:              false,
}
