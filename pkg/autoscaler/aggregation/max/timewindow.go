/*
Copyright 2020 The Knative Authors

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

package max

import (
	"math"
	"time"
)

// TimeWindow is a descending minima window whose indexes are calculated based
// on time.Time values.
type TimeWindow struct {
	window      *window
	granularity time.Duration
}

// NewTimeWindow creates a new TimeWindow.
func NewTimeWindow(duration, granularity time.Duration) *TimeWindow {
	buckets := int(math.Ceil(float64(duration) / float64(granularity)))
	return &TimeWindow{window: newWindow(buckets), granularity: granularity}
}

// Record records a value in the bucket derived from the given time.
func (t *TimeWindow) Record(now time.Time, value int32) {
	index := int(now.Unix()) / int(t.granularity.Seconds())
	t.window.Record(index, value)
}

// Current returns the current maximum value observed in the previous
// window duration.
func (t *TimeWindow) Current() int32 {
	return t.window.Current()
}
