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

	"github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"

	. "github.com/knative/serving/pkg/testing"
)

func TestResolveConcurrency(t *testing.T) {
	cases := []struct {
		name    string
		pa      *v1alpha1.PodAutoscaler
		cfgOpt  func(autoscaler.Config) *autoscaler.Config
		wantTgt float64
		wantTot float64
	}{{
		name:    "defaults",
		pa:      pa(),
		wantTgt: 100,
		wantTot: 100,
	}, {
		name: "with container concurrency 10 and TU=80%",
		pa:   pa(WithContainerConcurrency(10)),
		cfgOpt: func(c autoscaler.Config) *autoscaler.Config {
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
		wantTgt: 8,
	}, {
		name: "with container concurrency 1 and TU=80%",
		pa:   pa(WithContainerConcurrency(1)),
		cfgOpt: func(c autoscaler.Config) *autoscaler.Config {
			c.ContainerConcurrencyTargetFraction = 0.8
			return &c
		},
		wantTgt: 1, // Not permitting less than 1.
	}, {
		name:    "with container concurrency 1",
		pa:      pa(WithContainerConcurrency(1)),
		wantTgt: 1,
	}, {
		name:    "with container concurrency 10",
		pa:      pa(WithContainerConcurrency(10)),
		wantTgt: 10,
	}, {
		name:    "with target annotation 1",
		pa:      pa(WithTargetAnnotation("1")),
		wantTgt: 1,
	}, {
		name:    "with container concurrency greater than target annotation (ok)",
		pa:      pa(WithContainerConcurrency(10), WithTargetAnnotation("1")),
		wantTgt: 1,
		wantTot: 1,
	}, {
		name:    "with target annotation greater than container concurrency (ignore annotation for safety)",
		pa:      pa(WithContainerConcurrency(1), WithTargetAnnotation("10")),
		wantTgt: 1,
		wantTot: 1,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config
			if tc.cfgOpt != nil {
				cfg = tc.cfgOpt(*cfg)
			}
			if gotTgt, _ := ResolveConcurrency(tc.pa, cfg); gotTgt != tc.wantTgt {
				t.Errorf("ResolveTargetConcurrency(%v, %v) = %v, want %v", tc.pa, config, gotTgt, tc.wantTgt)
			}
		})
	}
}
