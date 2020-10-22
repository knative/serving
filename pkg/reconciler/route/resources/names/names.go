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

package names

import (
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/network"
)

// K8sService returns the name of the K8sService for a given route.
func K8sService(route kmeta.Accessor) string {
	return route.GetName()
}

// K8sServiceFullname returns the full name of the K8sService for a given route.
func K8sServiceFullname(route kmeta.Accessor) string {
	return network.GetServiceHostname(K8sService(route), route.GetNamespace())
}

// Ingress returns the name for the Ingress
// child resource for the given Route.
func Ingress(route kmeta.Accessor) string {
	return kmeta.ChildName(route.GetName(), "")
}

// Certificate returns the name for the Certificate
// child resource for the given Route.
func Certificate(route kmeta.Accessor) string {
	return "route-" + string(route.GetUID())
}
