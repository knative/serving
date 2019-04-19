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

// PopulationSampleClient implements autoscaler.SampleClient interface.
type PopulationSampleClient struct{}

// NewPopulationSampleClient returns a point to a new PopulationSampleClient object.
func NewPopulationSampleClient() *PopulationSampleClient {
	return &PopulationSampleClient{}
}

// SampleSize returns a sample size for given population.
func (c *PopulationSampleClient) SampleSize(population int) int {
	// TODO(yanweiguo): implement this.
	return 3
}
