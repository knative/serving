/*
Copyright 2018 The Knative Authors

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

package testing

import (
	"k8s.io/api/core/v1"
)

// GenEndpointsSubset generates the subsets of endpoints
func GenEndpointsSubset(hostsPerSubset, subsets int) []v1.EndpointSubset {
	resp := []v1.EndpointSubset{}
	if hostsPerSubset > 0 {
		addresses := GenAddresses(hostsPerSubset)
		subset := v1.EndpointSubset{Addresses: addresses}
		for s := 0; s < subsets; s++ {
			resp = append(resp, subset)
		}
		return resp
	}
	return resp
}

// GenAddresses generates endpoint addresses
func GenAddresses(hosts int) (endpoints []v1.EndpointAddress) {
	for i := 0; i < hosts; i++ {
		endpoints = append(endpoints, v1.EndpointAddress{})
	}
	return endpoints
}
