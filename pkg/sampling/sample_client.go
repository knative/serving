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

package sampling

import "math"

const (
	// criticalVSqr is the square of the critical value of the Normal distribution
	// for a confidence level of 95%.
	criticalVSqr = 1.96 * 1.96
	// marginOfErrorSqr is the square of margin of error. 5 is a usually used value
	// for MOE.
	marginOfErrorSqr = 5.0 * 5.0
	popVar           = 100.0
)

// PopulationSampleClient implements autoscaler.SampleClient interface. It uses the
// following formula for the sample size n:
//
// if N <= 3:
//   n = N
// else:
//   n = N*X / (N + X – 1), X = C^2 ­* σ^2 / MOE^2,
//
// where N is the population size, C is the critical value of the Normal distribution
// for a given confidence level of 95%, MOE is the margin of error and σ^2 is the
// population variance.
type PopulationSampleClient struct{}

// NewPopulationSampleClient returns a point to a new PopulationSampleClient object.
func NewPopulationSampleClient() *PopulationSampleClient {
	return &PopulationSampleClient{}
}

// SampleSize returns a sample size for given population.
func (c *PopulationSampleClient) SampleSize(pop int) int {
	if pop < 0 {
		return 0
	}
	if pop <= 3 {
		return pop
	}
	x := criticalVSqr * popVar / marginOfErrorSqr
	popf := float64(pop)
	return int(math.Ceil(popf * x / (popf + x - 1)))
}
