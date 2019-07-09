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
		expectErr:   "expected 1 <= -1 <= 2147483647: autoscaling.knative.dev/minScale",
	}, {
		name:        "maxScale is -1",
		annotations: map[string]string{MaxScaleAnnotationKey: "-1"},
		expectErr:   "expected 1 <= -1 <= 2147483647: autoscaling.knative.dev/maxScale",
	}, {
		name:        "minScale is foo",
		annotations: map[string]string{MinScaleAnnotationKey: "foo"},
		expectErr:   "expected 1 <= foo <= 2147483647: autoscaling.knative.dev/minScale",
	}, {
		name:        "maxScale is bar",
		annotations: map[string]string{MaxScaleAnnotationKey: "bar"},
		expectErr:   "expected 1 <= bar <= 2147483647: autoscaling.knative.dev/maxScale",
	}, {
		name:        "max/minScale is bar",
		annotations: map[string]string{MaxScaleAnnotationKey: "bar", MinScaleAnnotationKey: "bar"},
		expectErr:   "expected 1 <= bar <= 2147483647: autoscaling.knative.dev/maxScale, autoscaling.knative.dev/minScale",
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
		expectErr:   "maxScale=2 is less than minScale=5: autoscaling.knative.dev/maxScale, autoscaling.knative.dev/minScale",
	}, {
		name: "minScale is 0, maxScale is 0",
		annotations: map[string]string{
			MinScaleAnnotationKey: "0",
			MaxScaleAnnotationKey: "0",
		},
	}, {
		name:        "panic window percentange bad",
		annotations: map[string]string{PanicWindowPercentageAnnotationKey: "-1"},
		expectErr:   "expected 1 <= -1 <= 100: autoscaling.knative.dev/panicWindowPercentage",
	}, {
		name:        "panic window percentange bad2",
		annotations: map[string]string{PanicWindowPercentageAnnotationKey: "202"},
		expectErr:   "expected 1 <= 202 <= 100: autoscaling.knative.dev/panicWindowPercentage",
	}, {
		name:        "panic window percentange bad3",
		annotations: map[string]string{PanicWindowPercentageAnnotationKey: "fifty"},
		expectErr:   "invalid value: fifty: autoscaling.knative.dev/panicWindowPercentage",
	}, {
		name:        "panic window percentange good",
		annotations: map[string]string{PanicThresholdPercentageAnnotationKey: "210"},
	}, {
		name:        "panic threshold percentange bad2",
		annotations: map[string]string{PanicThresholdPercentageAnnotationKey: "109"},
		expectErr:   "expected 110 <= 109 <= 1000: autoscaling.knative.dev/panicThresholdPercentage",
	}, {
		name:        "panic threshold percentange bad2.5",
		annotations: map[string]string{PanicThresholdPercentageAnnotationKey: "10009"},
		expectErr:   "expected 110 <= 10009 <= 1000: autoscaling.knative.dev/panicThresholdPercentage",
	}, {
		name:        "panic threshold percentange bad3",
		annotations: map[string]string{PanicThresholdPercentageAnnotationKey: "fifty"},
		expectErr:   "invalid value: fifty: autoscaling.knative.dev/panicThresholdPercentage",
	}, {
		name:        "window invalid",
		annotations: map[string]string{WindowAnnotationKey: "jerry-was-a-racecar-driver"},
		expectErr:   "invalid value: jerry-was-a-racecar-driver: autoscaling.knative.dev/window",
	}, {
		name:        "window too short",
		annotations: map[string]string{WindowAnnotationKey: "1s"},
		expectErr:   "expected 6s <= 1s <= 1h0m0s: autoscaling.knative.dev/window",
	}, {
		name:        "window too long",
		annotations: map[string]string{WindowAnnotationKey: "365h"},
		expectErr:   "expected 6s <= 365h <= 1h0m0s: autoscaling.knative.dev/window",
	}, {
		name: "all together now fail",
		annotations: map[string]string{
			PanicThresholdPercentageAnnotationKey: "fifty",
			PanicWindowPercentageAnnotationKey:    "-11",
			MinScaleAnnotationKey:                 "-4",
			MaxScaleAnnotationKey:                 "never",
		},
		expectErr: "expected 1 <= -11 <= 100: autoscaling.knative.dev/panicWindowPercentage\nexpected 1 <= -4 <= 2147483647: autoscaling.knative.dev/minScale\nexpected 1 <= never <= 2147483647: autoscaling.knative.dev/maxScale\ninvalid value: fifty: autoscaling.knative.dev/panicThresholdPercentage",
	}, {
		name: "all together now, succeed",
		annotations: map[string]string{
			PanicThresholdPercentageAnnotationKey: "125",
			PanicWindowPercentageAnnotationKey:    "75",
			MinScaleAnnotationKey:                 "5",
			MaxScaleAnnotationKey:                 "8",
			WindowAnnotationKey:                   "1984s",
		},
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, want := ValidateAnnotations(c.annotations).Error(), c.expectErr; !reflect.DeepEqual(got, want) {
				t.Errorf("Err = %q, want: %q, diff:\n%s", got, want, cmp.Diff(got, want))
			}
		})
	}
}
