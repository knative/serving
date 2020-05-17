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
	corev1 "k8s.io/api/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// ReadyAddressCount returns the total number of addresses ready for the given endpoint.
func ReadyAddressCount(endpoints *corev1.Endpoints) int {
	var ready int
	for _, subset := range endpoints.Subsets {
		ready += len(subset.Addresses)
	}
	return ready
}

// NotReadyAddressCount returns the total number of addresses ready for the given endpoint.
func NotReadyAddressCount(endpoints *corev1.Endpoints) int {
	var notReady int
	for _, subset := range endpoints.Subsets {
		notReady += len(subset.NotReadyAddresses)
	}
	return notReady
}

// EndpointsCounter provides a count of currently ready and notReady pods. This
// information is used by UniScaler implementations to make scaling
// decisions. The interface prevents the UniScaler from needing to
// know how counts are performed.
// The int return value represents the number of pods that are ready
// to handle incoming requests.
// The error value is returned if the EndpointsCounter is unable to
// calculate a value.
type EndpointsCounter interface {
	ReadyCount() (int, error)
	NotReadyCount() (int, error)
}

type scopedEndpointCounter struct {
	endpointsLister corev1listers.EndpointsNamespaceLister
	serviceName     string
}

func (eac *scopedEndpointCounter) ReadyCount() (int, error) {
	endpoints, err := eac.endpointsLister.Get(eac.serviceName)
	if err != nil {
		return 0, err
	}
	return ReadyAddressCount(endpoints), nil
}

func (eac *scopedEndpointCounter) NotReadyCount() (int, error) {
	endpoints, err := eac.endpointsLister.Get(eac.serviceName)
	if err != nil {
		return 0, err
	}
	return NotReadyAddressCount(endpoints), nil
}

// NewScopedEndpointsCounter creates a EndpointsCounter that uses
// a count of endpoints for a namespace/serviceName as the value
// of ready pods. The values returned by ReadyCount() will vary
// over time.
// lister is used to retrieve endpoints for counting with the
// scope of namespace/serviceName.
func NewScopedEndpointsCounter(lister corev1listers.EndpointsLister, namespace, serviceName string) EndpointsCounter {
	return &scopedEndpointCounter{
		endpointsLister: lister.Endpoints(namespace),
		serviceName:     serviceName,
	}
}
