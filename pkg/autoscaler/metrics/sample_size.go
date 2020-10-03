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

package metrics

import "math"

const (
	// criticalValueSquared is the square of the critical value of the Normal distribution
	// for a confidence level of 95%.
	criticalValueSquared = 1.96 * 1.96
	// marginOfErrorSquared is the square of margin of error. 5 is a usually used value
	// for MOE.
	marginOfErrorSquared = 5.0 * 5.0
	// populationVariance is the variance we assume in the current population.
	populationVariance = 100.0

	// sampleSize is the sample size required to achieve the confidence
	// with the giving moe and populationVariance.
	// Since sampleSize is the number of samples we need in an unbounded population
	// we scale it according to the actual pod population.
	sampleSize = criticalValueSquared * populationVariance / marginOfErrorSquared
)

// populationMeanSampleSize uses the following formula for the sample size n:
//
// if N <= 3:
//   n = N
// else:
//   n = N*X / (N + X – 1), X = C^2 * σ^2 / MOE^2,
//
// where N is the population size, C is the critical value of the Normal distribution
// for a given confidence level of 95%, MOE is the margin of error and σ^2 is the
// population variance.
func populationMeanSampleSize(population float64) float64 {
	if population <= 3 {
		return math.Max(population, 0)
	}
	return math.Ceil(population * sampleSize / (population + sampleSize - 1))
}
