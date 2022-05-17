/*
Copyright 2022 The Knative Authors

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

package k8s

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// NameForPortNumber finds the name for a given port as defined by a Service.
func NameForPortNumber(svc *corev1.Service, portNumber int32) (string, error) {
	for _, port := range svc.Spec.Ports {
		if port.Port == portNumber {
			return port.Name, nil
		}
	}
	return "", fmt.Errorf("no port with number %d found", portNumber)
}

// PortNumberForName resolves a given name to a portNumber as defined by an EndpointSubset.
func PortNumberForName(sub corev1.EndpointSubset, portName string) (int32, error) {
	for _, subPort := range sub.Ports {
		if subPort.Name == portName {
			return subPort.Port, nil
		}
	}
	return 0, fmt.Errorf("no port for name %q found", portName)
}
