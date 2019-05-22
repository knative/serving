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

package resources

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// ParentResourceFromService returns the parent resource name from
// endpoints or k8s service resource.
// The function is based upon knowledge that all knative built services
// have `parent-resource-<svc-unique-suffix` format.
func ParentResourceFromService(name string) string {
	li := strings.LastIndex(name, "-")
	if li == -1 {
		// Presume same.
		return name
	}
	return name[:li]
}

type ReadyPodCounter interface {
	ReadyCount() (int, error)
}

type scopedEndpointCounter struct {
	endpointsLister corev1listers.EndpointsLister
	namespace       string
	name            string
}

func (eac *scopedEndpointCounter) ReadyCount() (int, error) {
	return fetchReadyAddressCount(eac.endpointsLister, eac.namespace, eac.name)
}

func NewScopedEndpointsCounter(lister corev1listers.EndpointsLister, namespace, name string) ReadyPodCounter {
	return &scopedEndpointCounter{
		endpointsLister: lister,
		namespace:       namespace,
		name:            name,
	}
}

type unscopedCounter struct {
	endpoints *corev1.Endpoints
}

func (uc *unscopedCounter) ReadyCount() (int, error) {
	return readyAddressCount(uc.endpoints), nil
}

func NewUnscopedEndpointsCounter(endpoints *corev1.Endpoints) ReadyPodCounter {
	return &unscopedCounter{
		endpoints: endpoints,
	}
}

// fetchReadyAddressCount fetches endpoints and returns the total number of addresses ready for them.
func fetchReadyAddressCount(lister corev1listers.EndpointsLister, ns, name string) (int, error) {
	endpoints, err := lister.Endpoints(ns).Get(name)
	if err != nil {
		return 0, err
	}
	return readyAddressCount(endpoints), nil
}

// readyAddressCount returns the total number of addresses ready for the given endpoint.
func readyAddressCount(endpoints *corev1.Endpoints) int {
	var total int
	for _, subset := range endpoints.Subsets {
		total += len(subset.Addresses)
	}
	return total
}
