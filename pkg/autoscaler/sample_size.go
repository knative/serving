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

import "math"

const (
	// criticalValueSquared is the square of the critical value of the Normal distribution
	// for a confidence level of 95%.
	criticalValueSquared = 1.96 * 1.96
	// marginOfErrorSquared is the square of margin of error. 5 is a usually used value
	// for MOE.
	marginOfErrorSquared = 5.0 * 5.0
	// σ2 is the population variance.
	σ2 = 100.0
)

// populationMeanSampleSize uses the following formula for the sample size n:
//
// if N <= 3:
//   n = N
// else:
//   n = N*X / (N + X – 1), X = C^2 ­* σ^2 / MOE^2,
//
// where N is the population size, C is the critical value of the Normal distribution
// for a given confidence level of 95%, MOE is the margin of error and σ^2 is the
// population variance.
func populationMeanSampleSize(population int) int {
	if population < 0 {
		return 0
	}
	if population <= 3 {
		return population
	}
	x := criticalValueSquared * σ2 / marginOfErrorSquared
	populationf := float64(population)
	return int(math.Ceil(populationf * x / (populationf + x - 1)))
}
