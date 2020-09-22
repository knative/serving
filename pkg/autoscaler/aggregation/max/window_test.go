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

package max_test

import (
	"reflect"
	"testing"

	"knative.dev/serving/pkg/autoscaler/aggregation/max"
)

func TestWindowMax(t *testing.T) {
	tests := []struct {
		name      string
		values    []float64
		indexFunc func(int) int
		expect    []float64
	}{{
		name:   "single value",
		values: []float64{1},
		expect: []float64{1},
	}, {
		name:   "ascending values",
		values: []float64{1, 2},
		expect: []float64{1, 2},
	}, {
		name:   "descending values",
		values: []float64{2, 1},
		expect: []float64{2, 2},
	}, {
		name:   "up, down, up",
		values: []float64{1, 2, 1},
		expect: []float64{1, 2, 2},
	}, {
		name:   "windowing out",
		values: []float64{5, 6, 5, 5, 5, 5, 5},
		expect: []float64{5, 6, 6, 6, 6, 6, 5},
	}, {
		name:   "windowing out with gaps",
		values: []float64{6, 5, 2, 1},
		indexFunc: func(i int) int {
			if i >= 3 {
				return i + 3
			}

			return i
		},
		expect: []float64{6, 6, 6, 2},
	}, {
		name:   "windowing out 2",
		values: []float64{5, 6, 5, 7, 5, 5, 1},
		expect: []float64{5, 6, 6, 7, 7, 7, 7},
	}, {
		name:   "windowing out 3",
		values: []float64{5, 8, 5, 7, 5, 5},
		expect: []float64{5, 8, 8, 8, 8, 8},
	}, {
		name:   "windowing out 4",
		values: []float64{5, 8, 5, 7, 5, 5, 1},
		expect: []float64{5, 8, 8, 8, 8, 8, 7},
	}, {
		name:   "windowing out 5",
		values: []float64{5, 8, 5, 7, 5, 5, 1, 4, 4, 4},
		expect: []float64{5, 8, 8, 8, 8, 8, 7, 7, 5, 5},
	}, {
		name:   "windowing out 6",
		values: []float64{5, 8, 5, 7, 5, 5, 1, 4, 4, 4, 4},
		expect: []float64{5, 8, 8, 8, 8, 8, 7, 7, 5, 5, 4},
	}, {
		name:   "windowing out 7",
		values: []float64{5, 8, 5, 7, 5, 5, 1, 4, 4, 4, 4, 9},
		expect: []float64{5, 8, 8, 8, 8, 8, 7, 7, 5, 5, 4, 9},
	}, {
		name:   "windowing out 8",
		values: []float64{5, 8, 5, 7, 5, 5, 1, 4, 4, 4, 4, 9, 3, 4, 2, 1, 0},
		expect: []float64{5, 8, 8, 8, 8, 8, 7, 7, 5, 5, 4, 9, 9, 9, 9, 9, 4},
	}, {
		name:   "multiple with same index, ascending",
		values: []float64{1, 2, 3, 4, 5, 6, 7},
		indexFunc: func(int) int {
			return 1
		},
		expect: []float64{1, 2, 3, 4, 5, 6, 7},
	}, {
		name:   "multiple with same index, descending",
		values: []float64{7, 6, 5, 4, 3, 2, 1},
		indexFunc: func(int) int {
			return 1
		},
		expect: []float64{7, 7, 7, 7, 7, 7, 7},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			max := max.NewWindow(5)

			indexFunc := func(i int) int { return i }
			if tt.indexFunc != nil {
				indexFunc = tt.indexFunc
			}

			current := make([]float64, 0, len(tt.expect))
			for i, v := range tt.values {
				max.Record(indexFunc(i), v)
				current = append(current, max.Current())
			}

			if got, want := current, tt.expect; !reflect.DeepEqual(got, want) {
				t.Errorf("Current() = %f, expected %f", got, want)
			}
		})
	}
}
