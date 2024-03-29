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

package net

import (
	"net"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/networking/pkg/apis/networking"
)

// healthyAddresses takes an endpoints object and a port name and return the set
// of addresses that implement this port.
func healthyAddresses(endpoints *corev1.Endpoints, portName string) sets.Set[string] {
	var addresses int
	for _, es := range endpoints.Subsets {
		for _, port := range es.Ports {
			if port.Name == portName {
				addresses += len(es.Addresses)
				break
			}
		}
	}

	ready := make(sets.Set[string], addresses)

	for _, es := range endpoints.Subsets {
		for _, port := range es.Ports {
			if port.Name == portName {
				for _, addr := range es.Addresses {
					ready.Insert(addr.IP)
				}
				break
			}
		}
	}

	return ready
}

// endpointsToDests takes an endpoints object and a port name and returns two sets of
// ready and non-ready l4 dests in the endpoints object which have that port.
func endpointsToDests(endpoints *corev1.Endpoints, portName string) (ready, notReady sets.Set[string]) {
	var readyAddresses, nonReadyAddresses int
	for _, es := range endpoints.Subsets {
		for _, port := range es.Ports {
			if port.Name == portName {
				readyAddresses += len(es.Addresses)
				nonReadyAddresses += len(es.NotReadyAddresses)
				break
			}
		}
	}

	ready = make(sets.Set[string], readyAddresses)
	notReady = make(sets.Set[string], nonReadyAddresses)

	for _, es := range endpoints.Subsets {
		for _, port := range es.Ports {
			if port.Name == portName {
				portStr := strconv.Itoa(int(port.Port))
				for _, addr := range es.Addresses {
					// Prefer IP as we can avoid a DNS lookup this way.
					ready.Insert(net.JoinHostPort(addr.IP, portStr))
				}
				for _, addr := range es.NotReadyAddresses {
					// Prefer IP as we can avoid a DNS lookup this way.
					notReady.Insert(net.JoinHostPort(addr.IP, portStr))
				}
				break
			}
		}
	}

	return ready, notReady
}

// getServicePort takes a service and a protocol and returns the port number of
// the port named for that protocol. If the port is not found then ok is false.
func getServicePort(protocol networking.ProtocolType, svc *corev1.Service) (int, bool) {
	wantName := networking.ServicePortName(protocol)
	for _, p := range svc.Spec.Ports {
		if p.Name == wantName {
			return int(p.Port), true
		}
	}

	return 0, false
}
