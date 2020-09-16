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

	asconfig "knative.dev/serving/pkg/autoscaler/config"
)

func TestValidateAnnotations(t *testing.T) {
	cases := []struct {
		name               string
		annotations        map[string]string
		expectErr          string
		allowInitScaleZero bool
	}{{
		name:        "nil annotations",
		annotations: nil,
	}, {
		name:        "empty annotations",
		annotations: map[string]string{},
	}, {
		name:        "invalid class",
		annotations: map[string]string{asconfig.ClassAnnotationKey: "unsupported.knative.dev"},
		expectErr:   "invalid value: unsupported.knative.dev: " + asconfig.ClassAnnotationKey,
	}, {
		name:        "non-knative.dev domain class",
		annotations: map[string]string{asconfig.ClassAnnotationKey: "some.other.domain"},
	}, {
		name:        "minScale is 0",
		annotations: map[string]string{asconfig.MinScaleAnnotationKey: "0"},
	}, {
		name:        "maxScale is 0",
		annotations: map[string]string{asconfig.MaxScaleAnnotationKey: "0"},
	}, {
		name:        "minScale is -1",
		annotations: map[string]string{asconfig.MinScaleAnnotationKey: "-1"},
		expectErr:   "expected 0 <= -1 <= 2147483647: " + asconfig.MinScaleAnnotationKey,
	}, {
		name:        "maxScale is huuuuuuuge",
		annotations: map[string]string{asconfig.MaxScaleAnnotationKey: "2147483648"},
		expectErr:   "expected 0 <= 2147483648 <= 2147483647: " + asconfig.MaxScaleAnnotationKey,
	}, {
		name:        "maxScale is -1",
		annotations: map[string]string{asconfig.MaxScaleAnnotationKey: "-1"},
		expectErr:   "expected 0 <= -1 <= 2147483647: " + asconfig.MaxScaleAnnotationKey,
	}, {
		name:        "minScale is foo",
		annotations: map[string]string{asconfig.MinScaleAnnotationKey: "foo"},
		expectErr:   "invalid value: foo: " + asconfig.MinScaleAnnotationKey,
	}, {
		name:        "maxScale is bar",
		annotations: map[string]string{asconfig.MaxScaleAnnotationKey: "bar"},
		expectErr:   "invalid value: bar: " + asconfig.MaxScaleAnnotationKey,
	}, {
		name:        "max/minScale is bar",
		annotations: map[string]string{asconfig.MaxScaleAnnotationKey: "bar", asconfig.MinScaleAnnotationKey: "bar"},
		expectErr:   "invalid value: bar: " + asconfig.MaxScaleAnnotationKey + ", " + asconfig.MinScaleAnnotationKey,
	}, {
		name:        "minScale is 5",
		annotations: map[string]string{asconfig.MinScaleAnnotationKey: "5"},
	}, {
		name:        "maxScale is 2",
		annotations: map[string]string{asconfig.MaxScaleAnnotationKey: "2"},
	}, {
		name:        "minScale is 2, maxScale is 5",
		annotations: map[string]string{asconfig.MinScaleAnnotationKey: "2", asconfig.MaxScaleAnnotationKey: "5"},
	}, {
		name:        "minScale is 5, maxScale is 2",
		annotations: map[string]string{asconfig.MinScaleAnnotationKey: "5", asconfig.MaxScaleAnnotationKey: "2"},
		expectErr:   "maxScale=2 is less than minScale=5: " + asconfig.MaxScaleAnnotationKey + ", " + asconfig.MinScaleAnnotationKey,
	}, {
		name: "minScale is 0, maxScale is 0",
		annotations: map[string]string{
			asconfig.MinScaleAnnotationKey: "0",
			asconfig.MaxScaleAnnotationKey: "0",
		},
	}, {
		name:        "panic window percentange bad",
		annotations: map[string]string{asconfig.PanicWindowPercentageAnnotationKey: "-1"},
		expectErr:   "expected 1 <= -1 <= 100: " + asconfig.PanicWindowPercentageAnnotationKey,
	}, {
		name:        "panic window percentange bad2",
		annotations: map[string]string{asconfig.PanicWindowPercentageAnnotationKey: "202"},
		expectErr:   "expected 1 <= 202 <= 100: " + asconfig.PanicWindowPercentageAnnotationKey,
	}, {
		name:        "panic window percentange bad3",
		annotations: map[string]string{asconfig.PanicWindowPercentageAnnotationKey: "fifty"},
		expectErr:   "invalid value: fifty: " + asconfig.PanicWindowPercentageAnnotationKey,
	}, {
		name:        "panic window percentange good",
		annotations: map[string]string{asconfig.PanicThresholdPercentageAnnotationKey: "210"},
	}, {
		name:        "panic threshold percentange bad2",
		annotations: map[string]string{asconfig.PanicThresholdPercentageAnnotationKey: "109"},
		expectErr:   "expected 110 <= 109 <= 1000: " + asconfig.PanicThresholdPercentageAnnotationKey,
	}, {
		name:        "panic threshold percentange bad2.5",
		annotations: map[string]string{asconfig.PanicThresholdPercentageAnnotationKey: "10009"},
		expectErr:   "expected 110 <= 10009 <= 1000: " + asconfig.PanicThresholdPercentageAnnotationKey,
	}, {
		name:        "panic threshold percentange bad3",
		annotations: map[string]string{asconfig.PanicThresholdPercentageAnnotationKey: "fifty"},
		expectErr:   "invalid value: fifty: " + asconfig.PanicThresholdPercentageAnnotationKey,
	}, {
		name:        "target negative",
		annotations: map[string]string{asconfig.TargetAnnotationKey: "-11"},
		expectErr:   "target -11 should be at least 0.01: " + asconfig.TargetAnnotationKey,
	}, {
		name:        "target 0",
		annotations: map[string]string{asconfig.TargetAnnotationKey: "0"},
		expectErr:   "target 0 should be at least 0.01: " + asconfig.TargetAnnotationKey,
	}, {
		name:        "target okay",
		annotations: map[string]string{asconfig.TargetAnnotationKey: "11"},
	}, {
		name:        "TBC negative",
		annotations: map[string]string{asconfig.TargetBurstCapacityKey: "-11"},
		expectErr:   "invalid value: -11: " + asconfig.TargetBurstCapacityKey,
	}, {
		name:        "TBC 0",
		annotations: map[string]string{asconfig.TargetBurstCapacityKey: "0"},
	}, {
		name:        "TBC 19880709",
		annotations: map[string]string{asconfig.TargetBurstCapacityKey: "19870709"},
	}, {
		name:        "TBC -1",
		annotations: map[string]string{asconfig.TargetBurstCapacityKey: "-1"},
	}, {
		name:        "TBC invalid",
		annotations: map[string]string{asconfig.TargetBurstCapacityKey: "qarashen"},
		expectErr:   "invalid value: qarashen: " + asconfig.TargetBurstCapacityKey,
	}, {
		name:        "TU too small",
		annotations: map[string]string{asconfig.TargetUtilizationPercentageKey: "0"},
		expectErr:   "expected 1 <= 0 <= 100: " + asconfig.TargetUtilizationPercentageKey,
	}, {
		name:        "TU too big",
		annotations: map[string]string{asconfig.TargetUtilizationPercentageKey: "101"},
		expectErr:   "expected 1 <= 101 <= 100: " + asconfig.TargetUtilizationPercentageKey,
	}, {
		name:        "TU invalid",
		annotations: map[string]string{asconfig.TargetUtilizationPercentageKey: "dghyak"},
		expectErr:   "invalid value: dghyak: " + asconfig.TargetUtilizationPercentageKey,
	}, {
		name:        "window invalid",
		annotations: map[string]string{asconfig.WindowAnnotationKey: "jerry-was-a-racecar-driver"},
		expectErr:   "invalid value: jerry-was-a-racecar-driver: " + asconfig.WindowAnnotationKey,
	}, {
		name:        "window too short",
		annotations: map[string]string{asconfig.WindowAnnotationKey: "1s"},
		expectErr:   "expected 6s <= 1s <= 1h0m0s: " + asconfig.WindowAnnotationKey,
	}, {
		name:        "window too long",
		annotations: map[string]string{asconfig.WindowAnnotationKey: "365h"},
		expectErr:   "expected 6s <= 365h <= 1h0m0s: " + asconfig.WindowAnnotationKey,
	}, {
		name: "annotation /window is invalid for class HPA and metric CPU",
		annotations: map[string]string{asconfig.WindowAnnotationKey: "7s",
			asconfig.ClassAnnotationKey: asconfig.HPA, asconfig.MetricAnnotationKey: asconfig.CPU},
		expectErr: fmt.Sprintf("invalid key name %q: \n%s for %s %s", asconfig.WindowAnnotationKey,
			asconfig.HPA, asconfig.MetricAnnotationKey, asconfig.CPU),
	}, {
		name:        "annotation /window is valid for class KPA",
		annotations: map[string]string{asconfig.WindowAnnotationKey: "7s", asconfig.ClassAnnotationKey: asconfig.KPA},
		expectErr:   "",
	}, {
		name:        "annotation /window is valid for other than HPA and KPA class",
		annotations: map[string]string{asconfig.WindowAnnotationKey: "7s", asconfig.ClassAnnotationKey: "test"},
		expectErr:   "",
	}, {
		name: "value too short and invalid class for /window annotation",
		annotations: map[string]string{asconfig.WindowAnnotationKey: "1s", asconfig.ClassAnnotationKey: asconfig.HPA,
			asconfig.MetricAnnotationKey: asconfig.CPU},
		expectErr: fmt.Sprintf("invalid key name %q: \n%s for %s %s", asconfig.WindowAnnotationKey,
			asconfig.HPA, asconfig.MetricAnnotationKey, asconfig.CPU),
	}, {
		name:        "value too long and valid class for /window annotation",
		annotations: map[string]string{asconfig.WindowAnnotationKey: "365h", asconfig.ClassAnnotationKey: asconfig.KPA},
		expectErr:   "expected 6s <= 365h <= 1h0m0s: " + asconfig.WindowAnnotationKey,
	}, {
		name: "invalid format and valid class for /window annotation",
		annotations: map[string]string{asconfig.WindowAnnotationKey: "jerry-was-a-racecar-driver",
			asconfig.ClassAnnotationKey: asconfig.KPA},
		expectErr: "invalid value: jerry-was-a-racecar-driver: " + asconfig.WindowAnnotationKey,
	}, {
		name:        "valid 0 last pod scaledown timeout",
		annotations: map[string]string{asconfig.ScaleToZeroPodRetentionPeriodKey: "0"},
	}, {
		name:        "valid positive last pod scaledown timeout",
		annotations: map[string]string{asconfig.ScaleToZeroPodRetentionPeriodKey: "21m31s"},
	}, {
		name:        "invalid positive last pod scaledown timeout",
		annotations: map[string]string{asconfig.ScaleToZeroPodRetentionPeriodKey: "311m"},
		expectErr:   "expected 0s <= 311m <= 1h0m0s: " + asconfig.ScaleToZeroPodRetentionPeriodKey,
	}, {
		name:        "invalid negative last pod scaledown timeout",
		annotations: map[string]string{asconfig.ScaleToZeroPodRetentionPeriodKey: "-42s"},
		expectErr:   "expected 0s <= -42s <= 1h0m0s: " + asconfig.ScaleToZeroPodRetentionPeriodKey,
	}, {
		name:        "invalid last pod scaledown timeout",
		annotations: map[string]string{asconfig.ScaleToZeroPodRetentionPeriodKey: "twenty-two-minutes-and-five-seconds"},
		expectErr:   "invalid value: twenty-two-minutes-and-five-seconds: " + asconfig.ScaleToZeroPodRetentionPeriodKey,
	}, {
		name: "all together now fail",
		annotations: map[string]string{
			asconfig.PanicThresholdPercentageAnnotationKey: "fifty",
			asconfig.PanicWindowPercentageAnnotationKey:    "-11",
			asconfig.MinScaleAnnotationKey:                 "-4",
			asconfig.MaxScaleAnnotationKey:                 "never",
		},
		expectErr: "expected 0 <= -4 <= 2147483647: " + asconfig.MinScaleAnnotationKey +
			"\nexpected 1 <= -11 <= 100: " + asconfig.PanicWindowPercentageAnnotationKey +
			"\ninvalid value: fifty: " + asconfig.PanicThresholdPercentageAnnotationKey +
			"\ninvalid value: never: " + asconfig.MaxScaleAnnotationKey,
	}, {
		name: "all together now, succeed",
		annotations: map[string]string{
			asconfig.PanicThresholdPercentageAnnotationKey: "125",
			asconfig.PanicWindowPercentageAnnotationKey:    "75",
			asconfig.MinScaleAnnotationKey:                 "5",
			asconfig.MaxScaleAnnotationKey:                 "8",
			asconfig.WindowAnnotationKey:                   "1984s",
		},
	}, {
		name:        "invalid metric for default class(KPA)",
		annotations: map[string]string{asconfig.MetricAnnotationKey: asconfig.CPU},
		expectErr:   "invalid value: cpu: " + asconfig.MetricAnnotationKey,
	}, {
		name:        "invalid metric for HPA class",
		annotations: map[string]string{asconfig.MetricAnnotationKey: "metrics", asconfig.ClassAnnotationKey: asconfig.HPA},
		expectErr:   "invalid value: metrics: " + asconfig.MetricAnnotationKey,
	}, {
		name:        "valid class KPA with metric RPS",
		annotations: map[string]string{asconfig.MetricAnnotationKey: asconfig.RPS},
	}, {
		name:        "valid class KPA with metric Concurrency",
		annotations: map[string]string{asconfig.MetricAnnotationKey: asconfig.Concurrency},
	}, {
		name:        "valid class HPA with metric CPU",
		annotations: map[string]string{asconfig.ClassAnnotationKey: asconfig.HPA, asconfig.MetricAnnotationKey: asconfig.CPU},
	}, {
		name:        "other than HPA and KPA class",
		annotations: map[string]string{asconfig.ClassAnnotationKey: "other", asconfig.MetricAnnotationKey: asconfig.RPS},
	}, {
		name:               "initial scale is zero but cluster doesn't allow",
		allowInitScaleZero: false,
		annotations:        map[string]string{asconfig.InitialScaleAnnotationKey: "0"},
		expectErr:          "invalid value: 0: autoscaling.knative.dev/initialScale",
	}, {
		name:               "initial scale is zero and cluster allows",
		allowInitScaleZero: true,
		annotations:        map[string]string{asconfig.InitialScaleAnnotationKey: "0"},
	}, {
		name:               "initial scale is greater than 0",
		allowInitScaleZero: false,
		annotations:        map[string]string{asconfig.InitialScaleAnnotationKey: "2"},
	}, {
		name:               "initial scale non-parseable",
		allowInitScaleZero: false,
		annotations:        map[string]string{asconfig.InitialScaleAnnotationKey: "invalid"},
		expectErr:          "invalid value: invalid: autoscaling.knative.dev/initialScale",
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, want := ValidateAnnotations(c.allowInitScaleZero, c.annotations).Error(), c.expectErr; !reflect.DeepEqual(got, want) {
				t.Errorf("\nErr = %q,\nwant: %q, diff(-want,+got):\n%s", got, want, cmp.Diff(want, got))
			}
		})
	}
}
