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

// ReadyAddressCount returns the total number of addresses ready for the given endpoint.
func ReadyAddressCount(endpoints *corev1.Endpoints) int {
	var total int
	for _, subset := range endpoints.Subsets {
		total += len(subset.Addresses)
	}
	return total
}

// ReadyPodCounter provides a count of currently ready pods. This
// information is used by UniScaler implementations to make scaling
// decisions. The interface prevents the UniScaler from needing to
// know how counts are performed.
// The int return value represents the number of pods that are ready
// to handle incoming requests.
// The error value is returned if the ReadyPodCounter is unable to
// calculate a value.
type ReadyPodCounter interface {
	ReadyCount() (int, error)
}

type scopedEndpointCounter struct {
	endpointsLister corev1listers.EndpointsLister
	namespace       string
	serviceName     string
}

func (eac *scopedEndpointCounter) ReadyCount() (int, error) {
	endpoints, err := eac.endpointsLister.Endpoints(eac.namespace).Get(eac.serviceName)
	if err != nil {
		return 0, err
	}
	return ReadyAddressCount(endpoints), nil
}

// NewScopedEndpointsCounter creates a ReadyPodCounter that uses
// a count of endpoints for a namespace/serviceName as the value
// of ready pods. The values returned by ReadyCount() will vary
// over time.
// lister is used to retrieve endpoints for counting with the
// scope of namespace/serviceName.
func NewScopedEndpointsCounter(lister corev1listers.EndpointsLister, namespace, serviceName string) ReadyPodCounter {
	return &scopedEndpointCounter{
		endpointsLister: lister,
		namespace:       namespace,
		serviceName:     serviceName,
	}
}
