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

	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/config/sharedconfig"

	. "knative.dev/serving/pkg/testing"
)

func TestResolveMetricTarget(t *testing.T) {
	cases := []struct {
		name       string
		pa         *v1alpha1.PodAutoscaler
		cfgOpt     func(sharedconfig.Config) *sharedconfig.Config
		wantTarget float64
		wantTotal  float64
	}{{
		name:       "defaults",
		pa:         pa(),
		wantTarget: 100,
		wantTotal:  100,
	}, {
		name: "default CC + 80% TU",
		pa:   pa(),
		cfgOpt: func(c sharedconfig.Config) *sharedconfig.Config {
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
		wantTarget: 80,
		wantTotal:  100,
	}, {
		name: "non-default CC and TU",
		pa:   pa(),
		cfgOpt: func(c sharedconfig.Config) *sharedconfig.Config {
			c.ContainerConcurrencyTargetFraction = 0.3
			c.ContainerConcurrencyTargetDefault = 2
			return &c
		},
		wantTarget: 0.6,
		wantTotal:  2,
	}, {
		name: "with container concurrency 12 and TU=80%, but TU annotation 75%",
		pa:   pa(WithPAContainerConcurrency(12), WithTUAnnotation("75")),
		cfgOpt: func(c sharedconfig.Config) *sharedconfig.Config {
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
		wantTarget: 9,
		wantTotal:  12,
	}, {
		name: "with container concurrency 10 and TU=80%",
		pa:   pa(WithPAContainerConcurrency(10)),
		cfgOpt: func(c sharedconfig.Config) *sharedconfig.Config {
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
		wantTarget: 8,
		wantTotal:  10,
	}, {
		name: "with container concurrency 1 and TU=80%",
		pa:   pa(WithPAContainerConcurrency(1)),
		cfgOpt: func(c sharedconfig.Config) *sharedconfig.Config {
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
		wantTarget: 0.8,
		wantTotal:  1,
	}, {
		name:       "with container concurrency 1",
		pa:         pa(WithPAContainerConcurrency(1)),
		wantTarget: 1,
		wantTotal:  1,
	}, {
		name: "with container concurrency 10 and TU=80%",
		pa:   pa(WithPAContainerConcurrency(10)),
		cfgOpt: func(c sharedconfig.Config) *sharedconfig.Config {
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
		wantTarget: 8,
		wantTotal:  10,
	}, {
		name:       "with container concurrency 10",
		pa:         pa(WithPAContainerConcurrency(10)),
		wantTarget: 10,
		wantTotal:  10,
	}, {
		name:       "with target annotation 1",
		pa:         pa(WithTargetAnnotation("1")),
		wantTarget: 1,
		wantTotal:  1,
	}, {
		name: "with target annotation 1 and TU=0.1%",
		pa:   pa(WithTargetAnnotation("1")),
		cfgOpt: func(c sharedconfig.Config) *sharedconfig.Config {
			c.ContainerConcurrencyTargetFraction = 0.001
			return &c
		},
		wantTarget: autoscaling.TargetMin,
		wantTotal:  1,
	}, {
		name: "with target annotation 1 and TU=75%",
		pa:   pa(WithTargetAnnotation("1")),
		cfgOpt: func(c sharedconfig.Config) *sharedconfig.Config {
			c.ContainerConcurrencyTargetFraction = 0.75
			return &c
		},
		wantTarget: 0.75,
		wantTotal:  1,
	}, {
		name: "with target annotation 10 and TU=75%",
		pa:   pa(WithTargetAnnotation("10")),
		cfgOpt: func(c sharedconfig.Config) *sharedconfig.Config {
			c.ContainerConcurrencyTargetFraction = 0.75
			return &c
		},
		wantTarget: 7.5,
		wantTotal:  10,
	}, {
		name:       "with container concurrency greater than target annotation (ok)",
		pa:         pa(WithPAContainerConcurrency(10), WithTargetAnnotation("1")),
		wantTarget: 1,
		wantTotal:  1,
	}, {
		name:       "with target annotation greater than default (ok)",
		pa:         pa(WithTargetAnnotation("500")),
		wantTarget: 500,
		wantTotal:  500,
	}, {
		name:       "with target annotation greater than container concurrency (ignore annotation for safety)",
		pa:         pa(WithPAContainerConcurrency(1), WithTargetAnnotation("10")),
		wantTarget: 1,
		wantTotal:  1,
	}, {
		name:       "RPS: defaults",
		pa:         pa(WithMetricAnnotation(autoscaling.RPS), WithPAContainerConcurrency(1)),
		wantTarget: 140,
		wantTotal:  200,
	}, {
		name:       "RPS: with target annotation 1",
		pa:         pa(WithMetricAnnotation(autoscaling.RPS), WithTargetAnnotation("1")),
		wantTarget: 0.7,
		wantTotal:  1,
	}, {
		name:       "RPS: with TU annotation 75%",
		pa:         pa(WithMetricAnnotation(autoscaling.RPS), WithTUAnnotation("75")),
		wantTarget: 150,
		wantTotal:  200,
	}, {
		name:       "RPS: with target annotation greater than default",
		pa:         pa(WithMetricAnnotation(autoscaling.RPS), WithTargetAnnotation("300")),
		wantTarget: 210,
		wantTotal:  300,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config
			if tc.cfgOpt != nil {
				cfg = tc.cfgOpt(*cfg)
			}
			gotTgt, gotTot := ResolveMetricTarget(tc.pa, cfg)
			if gotTgt != tc.wantTarget || gotTot != tc.wantTotal {
				t.Errorf("ResolveMetricTarget(%#v, %#v) = (%v, %v), want (%v, %v)",
					tc.pa, config, gotTgt, gotTot, tc.wantTarget, tc.wantTotal)
			}
		})
	}
}
