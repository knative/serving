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
func GetDomainConfigMapName() string {
	return "config-domain"
}

func GetNetworkConfigMapName() string {
	return "config-network"
}

func GetObservabilityConfigMapName() string {
	return "config-observability"
}

// Various functions for naming the resources for consistency
func GetServingNamespaceName(ns string) string {
	// We create resources in the same namespace as the Knative Serving resources by default.
	// TODO(mattmoor): Expose a knob for creating resources in an alternate namespace.
	return ns
}

func GetRevisionDeploymentName(u *v1alpha1.Revision) string {
	return u.Name + "-deployment"
}

func GetRevisionAutoscalerName(u *v1alpha1.Revision) string {
	return u.Name + "-autoscaler"
}

func GetRevisionVPAName(u *v1alpha1.Revision) string {
	return u.Name + "-vpa"
}

func GetServingK8SServiceNameForRevision(u *v1alpha1.Revision) string {
	return u.Name + "-service"
}

func GetServingK8SServiceNameForRoute(u *v1alpha1.Route) string {
	return u.Name + "-service"
}

func GetVirtualServiceName(u *v1alpha1.Route) string {
	return u.Name + "-istio"
}

func GetServingK8SGatewayFullname() string {
	return GetK8SServiceFullname("knative-shared-gateway", "knative-serving")
}

func GetServingK8SServiceNameForObj(name string) string {
	return name + "-service"
}

func GetServingK8SServiceFullnameForRoute(u *v1alpha1.Route) string {
	return GetK8SServiceFullname(GetServingK8SServiceNameForRoute(u), u.Namespace)
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

func GetConfigurationHeader() string {
	return "Knative-Serving-Configuration"
}

func GetRevisionHeaderNamespace() string {
	return "Knative-Serving-Namespace"
}
