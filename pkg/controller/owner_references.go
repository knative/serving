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
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	serviceControllerKind = v1alpha1.SchemeGroupVersion.WithKind("Service")
	routeControllerKind   = v1alpha1.SchemeGroupVersion.WithKind("Route")
	configControllerKind  = v1alpha1.SchemeGroupVersion.WithKind("Configuration")
	revControllerKind     = v1alpha1.SchemeGroupVersion.WithKind("Revision")
)

// NewServiceControllerRef creates an OwnerReference pointing to the given Service.
func NewServiceControllerRef(service *v1alpha1.Service) *metav1.OwnerReference {
	return metav1.NewControllerRef(service, serviceControllerKind)
}

// NewRouteControllerRef creates an OwnerReference pointing to the given Service.
func NewRouteControllerRef(route *v1alpha1.Route) *metav1.OwnerReference {
	return metav1.NewControllerRef(route, routeControllerKind)
}

// NewConfigurationControllerRef creates an OwnerReference pointing to the given Service.
func NewConfigurationControllerRef(config *v1alpha1.Configuration) *metav1.OwnerReference {
	return metav1.NewControllerRef(config, configControllerKind)
}

// NewRevisionControllerRef creates an OwnerReference pointing to the given Service.
func NewRevisionControllerRef(rev *v1alpha1.Revision) *metav1.OwnerReference {
	return metav1.NewControllerRef(rev, revControllerKind)
}
