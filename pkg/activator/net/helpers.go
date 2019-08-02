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

package activator

import (
	"net"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/serving/pkg/apis/networking"
)

// EndpointsToDests takes an endpoints object and a port name and returns a list
// of l4 dests in the endpoints object which have that port
func EndpointsToDests(endpoints *corev1.Endpoints) []string {
	ret := []string{}

	for _, es := range endpoints.Subsets {
		for _, port := range es.Ports {
			if port.Name == probePortName {
				portStr := strconv.Itoa(int(port.Port))
				for _, addr := range es.Addresses {
					// Prefer IP as we can avoid a DNS lookup this way
					ret = append(ret, net.JoinHostPort(addr.IP, portStr))
				}
			}
		}
	}

	return ret
}

// GetServicePort takes a service and a protocol and returns the port number of
// the port named for that protocol. If the port is not found then ok is false.
func GetServicePort(protocol networking.ProtocolType, svc *corev1.Service) (port int, ok bool) {
	port = 0
	ok = false
	wantName := networking.ServicePortName(protocol)
	for _, p := range svc.Spec.Ports {
		if p.Name == wantName {
			port = int(p.Port)
			ok = true
			return
		}
	}
	return
}
