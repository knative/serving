/*
Copyright 2025 The Knative Authors

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

package test

import (
	"context"
	"fmt"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

func ReadyAddressCount(slices []discoveryv1.EndpointSlice) int {
	count := 0
	for _, slice := range slices {
		count += len(ReadyAddresses(slice))
	}
	return count
}

func EndpointSlicesForService(k kubernetes.Interface, namespace, name string) ([]discoveryv1.EndpointSlice, error) {
	listOpts := metav1.ListOptions{
		LabelSelector: labels.Set{
			discoveryv1.LabelServiceName: name,
		}.String(),
	}

	slicesClient := k.DiscoveryV1().EndpointSlices(namespace)
	slices, err := slicesClient.List(context.Background(), listOpts)
	if err != nil {
		return nil, fmt.Errorf(`error: getting endpoint slices for service "%s/%s": %w`, namespace, name, err)
	}

	return slices.Items, nil
}

// ReadyAddresses returns an iterator over ready endpoint addresses.
func ReadyAddresses(slice discoveryv1.EndpointSlice) []string {
	var res []string
	for _, ep := range slice.Endpoints {
		if ep.Conditions.Ready != nil && !(*ep.Conditions.Ready) {
			continue
		}
		res = append(res, ep.Addresses...)
	}
	return res
}
