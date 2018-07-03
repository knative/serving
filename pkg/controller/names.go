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

package controller

import (
	"fmt"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

func GetK8SServiceFullname(name string, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace)
}

// Various functions for naming the resources for consistency
func GetServingNamespaceName(ns string) string {
	// We create resources in the same namespace as the Knative Serving resources by default.
	// TODO(mattmoor): Expose a knob for creating resources in an alternate namespace.
	return ns
}

func GetServingK8SServiceNameForObj(name string) string {
	return name + "-service"
}

func GetServiceConfigurationName(u *v1alpha1.Service) string {
	return u.Name
}

func GetServiceRouteName(u *v1alpha1.Service) string {
	return u.Name
}

func GetServingK8SActivatorServiceName() string {
	return "activator-service"
}

func GetRevisionHeaderName() string {
	return "Knative-Serving-Revision"
}

func GetRevisionHeaderNamespace() string {
	return "Knative-Serving-Namespace"
}
