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

package autoscaling

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestValidateScaleBoundAnnotations(t *testing.T) {
	cases := []struct {
		name        string
		annotations map[string]string
		expectErr   string
	}{{
		name:        "nil annotations",
		annotations: nil,
	}, {
		name:        "empty annotations",
		annotations: map[string]string{},
	}, {
		name:        "minScale is 0",
		annotations: map[string]string{MinScaleAnnotationKey: "0"},
	}, {
		name:        "maxScale is 0",
		annotations: map[string]string{MaxScaleAnnotationKey: "0"},
	}, {
		name:        "minScale is -1",
		annotations: map[string]string{MinScaleAnnotationKey: "-1"},
		expectErr:   "expected 1 <= -1 <= 2147483647: " + MinScaleAnnotationKey,
	}, {
		name:        "maxScale is -1",
		annotations: map[string]string{MaxScaleAnnotationKey: "-1"},
		expectErr:   "expected 1 <= -1 <= 2147483647: " + MaxScaleAnnotationKey,
	}, {
		name:        "minScale is foo",
		annotations: map[string]string{MinScaleAnnotationKey: "foo"},
		expectErr:   "expected 1 <= foo <= 2147483647: " + MinScaleAnnotationKey,
	}, {
		name:        "maxScale is bar",
		annotations: map[string]string{MaxScaleAnnotationKey: "bar"},
		expectErr:   "expected 1 <= bar <= 2147483647: " + MaxScaleAnnotationKey,
	}, {
		name:        "max/minScale is bar",
		annotations: map[string]string{MaxScaleAnnotationKey: "bar", MinScaleAnnotationKey: "bar"},
		expectErr:   "expected 1 <= bar <= 2147483647: " + MaxScaleAnnotationKey + ", " + MinScaleAnnotationKey,
	}, {
		name:        "minScale is 5",
		annotations: map[string]string{MinScaleAnnotationKey: "5"},
	}, {
		name:        "maxScale is 2",
		annotations: map[string]string{MaxScaleAnnotationKey: "2"},
	}, {
		name:        "minScale is 2, maxScale is 5",
		annotations: map[string]string{MinScaleAnnotationKey: "2", MaxScaleAnnotationKey: "5"},
	}, {
		name:        "minScale is 5, maxScale is 2",
		annotations: map[string]string{MinScaleAnnotationKey: "5", MaxScaleAnnotationKey: "2"},
		expectErr:   "maxScale=2 is less than minScale=5: " + MaxScaleAnnotationKey + ", " + MinScaleAnnotationKey,
	}, {
		name: "minScale is 0, maxScale is 0",
		annotations: map[string]string{
			MinScaleAnnotationKey: "0",
			MaxScaleAnnotationKey: "0",
		},
	}, {
		name:        "panic window percentange bad",
		annotations: map[string]string{PanicWindowPercentageAnnotationKey: "-1"},
		expectErr:   "expected 1 <= -1 <= 100: " + PanicWindowPercentageAnnotationKey,
	}, {
		name:        "panic window percentange bad2",
		annotations: map[string]string{PanicWindowPercentageAnnotationKey: "202"},
		expectErr:   "expected 1 <= 202 <= 100: " + PanicWindowPercentageAnnotationKey,
	}, {
		name:        "panic window percentange bad3",
		annotations: map[string]string{PanicWindowPercentageAnnotationKey: "fifty"},
		expectErr:   "invalid value: fifty: " + PanicWindowPercentageAnnotationKey,
	}, {
		name:        "panic window percentange good",
		annotations: map[string]string{PanicThresholdPercentageAnnotationKey: "210"},
	}, {
		name:        "panic threshold percentange bad2",
		annotations: map[string]string{PanicThresholdPercentageAnnotationKey: "109"},
		expectErr:   "expected 110 <= 109 <= 1000: " + PanicThresholdPercentageAnnotationKey,
	}, {
		name:        "panic threshold percentange bad2.5",
		annotations: map[string]string{PanicThresholdPercentageAnnotationKey: "10009"},
		expectErr:   "expected 110 <= 10009 <= 1000: " + PanicThresholdPercentageAnnotationKey,
	}, {
		name:        "panic threshold percentange bad3",
		annotations: map[string]string{PanicThresholdPercentageAnnotationKey: "fifty"},
		expectErr:   "invalid value: fifty: " + PanicThresholdPercentageAnnotationKey,
	}, {
		name:        "target negative",
		annotations: map[string]string{TargetAnnotationKey: "-11"},
		expectErr:   "invalid value: -11: " + TargetAnnotationKey,
	}, {
		name:        "target 0",
		annotations: map[string]string{TargetAnnotationKey: "0"},
		expectErr:   "invalid value: 0: " + TargetAnnotationKey,
	}, {
		name:        "target okay",
		annotations: map[string]string{TargetAnnotationKey: "11"},
	}, {
		name:        "TBC negative",
		annotations: map[string]string{TargetBurstCapacityKey: "-11"},
		expectErr:   "invalid value: -11: " + TargetBurstCapacityKey,
	}, {
		name:        "TBC 0",
		annotations: map[string]string{TargetBurstCapacityKey: "0"},
	}, {
		name:        "TBC 19880709",
		annotations: map[string]string{TargetBurstCapacityKey: "19870709"},
	}, {
		name:        "TBC -1",
		annotations: map[string]string{TargetBurstCapacityKey: "-1"},
	}, {
		name:        "TBC invalid",
		annotations: map[string]string{TargetBurstCapacityKey: "qarashen"},
		expectErr:   "invalid value: qarashen: " + TargetBurstCapacityKey,
	}, {
		name:        "TU too small",
		annotations: map[string]string{TargetUtilizationPercentageKey: "0"},
		expectErr:   "expected 1 <= 0 <= 100: " + TargetUtilizationPercentageKey,
	}, {
		name:        "TU too big",
		annotations: map[string]string{TargetUtilizationPercentageKey: "101"},
		expectErr:   "expected 1 <= 101 <= 100: " + TargetUtilizationPercentageKey,
	}, {
		name:        "TU invalid",
		annotations: map[string]string{TargetUtilizationPercentageKey: "dghyak"},
		expectErr:   "invalid value: dghyak: " + TargetUtilizationPercentageKey,
	}, {
		name:        "window invalid",
		annotations: map[string]string{WindowAnnotationKey: "jerry-was-a-racecar-driver"},
		expectErr:   "invalid value: jerry-was-a-racecar-driver: " + WindowAnnotationKey,
	}, {
		name:        "window too short",
		annotations: map[string]string{WindowAnnotationKey: "1s"},
		expectErr:   "expected 6s <= 1s <= 1h0m0s: " + WindowAnnotationKey,
	}, {
		name:        "window too long",
		annotations: map[string]string{WindowAnnotationKey: "365h"},
		expectErr:   "expected 6s <= 365h <= 1h0m0s: " + WindowAnnotationKey,
	}, {
		name:        "annotation /window is invalid for class HPA and metric CPU",
		annotations: map[string]string{WindowAnnotationKey: "7s", ClassAnnotationKey: HPA, MetricAnnotationKey: CPU},
		expectErr:   fmt.Sprintf(`invalid key name %q: %s for %s %s`, WindowAnnotationKey, HPA, MetricAnnotationKey, CPU),
	}, {
		name:        "annotation /window is valid for class KPA",
		annotations: map[string]string{WindowAnnotationKey: "7s", ClassAnnotationKey: KPA},
		expectErr:   "",
	}, {
		name:        "annotation /window is valid for class HPA and metric RPS",
		annotations: map[string]string{WindowAnnotationKey: "7s", ClassAnnotationKey: HPA, MetricAnnotationKey: RPS},
		expectErr:   "",
	}, {
		name:        "annotation /window is valid for class HPA and metric Concurrency",
		annotations: map[string]string{WindowAnnotationKey: "7s", ClassAnnotationKey: HPA, MetricAnnotationKey: Concurrency},
		expectErr:   "",
	}, {
		name:        "annotation /window is valid for other than HPA and KPA class",
		annotations: map[string]string{WindowAnnotationKey: "7s", ClassAnnotationKey: "test"},
		expectErr:   "",
	}, {
		name:        "value too short and invalid class for /window annotation",
		annotations: map[string]string{WindowAnnotationKey: "1s", ClassAnnotationKey: HPA, MetricAnnotationKey: CPU},
		expectErr:   fmt.Sprintf(`invalid key name %q: %s for %s %s`, WindowAnnotationKey, HPA, MetricAnnotationKey, CPU),
	}, {
		name:        "value too long and valid class for /window annotation",
		annotations: map[string]string{WindowAnnotationKey: "365h", ClassAnnotationKey: KPA},
		expectErr:   "expected 6s <= 365h <= 1h0m0s: " + WindowAnnotationKey,
	}, {
		name:        "invalid format and valid class for /window annotation",
		annotations: map[string]string{WindowAnnotationKey: "jerry-was-a-racecar-driver", ClassAnnotationKey: KPA},
		expectErr:   "invalid value: jerry-was-a-racecar-driver: " + WindowAnnotationKey,
	}, {
		name: "all together now fail",
		annotations: map[string]string{
			PanicThresholdPercentageAnnotationKey: "fifty",
			PanicWindowPercentageAnnotationKey:    "-11",
			MinScaleAnnotationKey:                 "-4",
			MaxScaleAnnotationKey:                 "never",
		},
		expectErr: "expected 1 <= -11 <= 100: " + PanicWindowPercentageAnnotationKey + "\nexpected 1 <= -4 <= 2147483647: " + MinScaleAnnotationKey + "\nexpected 1 <= never <= 2147483647: " + MaxScaleAnnotationKey + "\ninvalid value: fifty: " + PanicThresholdPercentageAnnotationKey,
	}, {
		name: "all together now, succeed",
		annotations: map[string]string{
			PanicThresholdPercentageAnnotationKey: "125",
			PanicWindowPercentageAnnotationKey:    "75",
			MinScaleAnnotationKey:                 "5",
			MaxScaleAnnotationKey:                 "8",
			WindowAnnotationKey:                   "1984s",
		},
	}, {
		name:        "invalid metric for default class(KPA)",
		annotations: map[string]string{MetricAnnotationKey: CPU},
		expectErr:   "invalid value: cpu: " + MetricAnnotationKey,
	}, {
		name:        "invalid metric for HPA class",
		annotations: map[string]string{MetricAnnotationKey: "metrics", ClassAnnotationKey: HPA},
		expectErr:   "invalid value: metrics: " + MetricAnnotationKey,
	}, {
		name:        "valid class KPA with metric RPS",
		annotations: map[string]string{MetricAnnotationKey: RPS},
	}, {
		name:        "valid class KPA with metric Concurrency",
		annotations: map[string]string{MetricAnnotationKey: Concurrency},
	}, {
		name:        "valid class HPA with metric Concurrency",
		annotations: map[string]string{ClassAnnotationKey: HPA, MetricAnnotationKey: Concurrency},
	}, {
		name:        "valid class HPA with metric CPU",
		annotations: map[string]string{ClassAnnotationKey: HPA, MetricAnnotationKey: CPU},
	}, {
		name:        "valid class HPA with metric RPS",
		annotations: map[string]string{ClassAnnotationKey: HPA, MetricAnnotationKey: RPS},
	}, {
		name:        "other than HPA and KPA class",
		annotations: map[string]string{ClassAnnotationKey: "other", MetricAnnotationKey: RPS},
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, want := ValidateAnnotations(c.annotations).Error(), c.expectErr; !reflect.DeepEqual(got, want) {
				t.Errorf("Err = %q, want: %q, diff:\n%s", got, want, cmp.Diff(got, want))
			}
		})
	}
}
