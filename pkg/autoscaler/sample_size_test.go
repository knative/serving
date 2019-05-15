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

package autoscaler

import (
	"testing"
)

func TestPopulationMeanSampleSize(t *testing.T) {
	testCases := []struct {
		popSize        int
		wantSampleSize int
	}{{
		popSize:        0,
		wantSampleSize: 0,
	}, {
		popSize:        1,
		wantSampleSize: 1,
	}, {
		popSize:        2,
		wantSampleSize: 2,
	}, {
		popSize:        5,
		wantSampleSize: 4,
	}, {
		popSize:        10,
		wantSampleSize: 7,
	}, {
		popSize:        100,
		wantSampleSize: 14,
	}, {
		popSize:        1000,
		wantSampleSize: 16,
	}}

	for _, testCase := range testCases {
		if got, want := populationMeanSampleSize(testCase.popSize), testCase.wantSampleSize; got != want {
			t.Errorf("client.SampleSize(%v) = %v, want %v", testCase.popSize, got, want)
		}
	}
}
