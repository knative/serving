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
	"knative.dev/serving/pkg/reconciler/route/config"
)

// Filter is used for applying a function to a service
type Filter func(service *corev1.Service) bool

// FilterService applies a filter to the list of services and return the services that are accepted
func FilterService(services []*corev1.Service, acceptFilter Filter) []*corev1.Service {
	var filteredServices []*corev1.Service

	for i := range services {
		service := services[i]
		if acceptFilter(service) {
			filteredServices = append(filteredServices, service)
		}
	}

	return filteredServices
}

// Filter functions

// IsClusterLocalService returns whether a service is cluster local.
func IsClusterLocalService(svc *corev1.Service) bool {
	if visibility, ok := svc.GetLabels()[config.VisibilityLabelKey]; ok {
		return config.VisibilityClusterLocal == visibility
	}

	return false
}
