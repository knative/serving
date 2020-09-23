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
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"
)

func TestTimedWindowMax(t *testing.T) {
	type entry struct {
		time  time.Time
		value float64
	}

	now := time.Now()

	tests := []struct {
		name   string
		expect float64
		values []entry
	}{{
		name: "single value",
		values: []entry{{
			time:  now,
			value: 5,
		}},
		expect: 5,
	}, {
		name: "two values in same second",
		values: []entry{{
			time:  now,
			value: 6,
		}, {
			time:  now.Add(500 * time.Millisecond),
			value: 5,
		}},
		expect: 6,
	}, {
		name: "two values",
		values: []entry{{
			time:  now,
			value: 5,
		}, {
			time:  now.Add(1 * time.Second),
			value: 8,
		}},
		expect: 8,
	}, {
		name: "time gap",
		values: []entry{{
			time:  now,
			value: 5,
		}, {
			time:  now.Add(6 * time.Second),
			value: 4,
		}},
		expect: 4,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewTimeWindow(5*time.Second, 1*time.Second)

			for _, v := range tt.values {
				m.Record(v.time, v.value)
			}

			if got, want := m.Current(), tt.expect; got != want {
				t.Errorf("Current() = %f, expected %f", got, want)
			}
		})
	}
}

func BenchmarkLargeTimeWindowCreate(b *testing.B) {
	for _, duration := range []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute, 45 * time.Minute} {
		b.Run(fmt.Sprintf("duration-%v", duration), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = NewTimeWindow(duration, 1*time.Second)
			}
		})
	}
}

func BenchmarkLargeTimeWindowRecord(b *testing.B) {
	w := NewTimeWindow(45*time.Minute, 1*time.Second)
	now := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now = now.Add(1 * time.Second)
		w.Record(now, rand.Float64())
	}
}

func BenchmarkLargeTimeWindowAscendingRecord(b *testing.B) {
	w := NewTimeWindow(45*time.Minute, 1*time.Second)
	now := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now = now.Add(1 * time.Second)
		w.Record(now, float64(i))
	}
}

func BenchmarkLargeTimeWindowDescendingRecord(b *testing.B) {
	for _, duration := range []time.Duration{5, 15, 30, 45} {
		b.Run(fmt.Sprintf("duration-%d-minutes", duration), func(b *testing.B) {
			w := NewTimeWindow(duration*time.Minute, 1*time.Second)
			now := time.Now()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				now = now.Add(1 * time.Second)
				w.Record(now, float64(math.MaxInt32-i))
			}
		})
	}
}
