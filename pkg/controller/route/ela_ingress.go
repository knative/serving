/*
Copyright 2018 Google LLC

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

package route

import (
	"fmt"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"

	v1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// MakeRouteIngress creates an ingress rule, owned by the provided v1alpha1.Route. This ingress rule
// targets Istio by using the simple placeholder service name. All the routing actually happens in
// the route rules.
func MakeRouteIngress(u *v1alpha1.Route, namespace string, domain string) *v1beta1.Ingress {
	// We used to have a distinct service, but in the ela world, use the
	// name for serviceID too.

	// Construct hostnames the ingress accepts traffic for. If traffic targets
	// are named, allow the wildcard dns record as well.
	hostRules := []string{domain}
	appendWildcardDNS := false
	for _, tt := range u.Spec.Traffic {
		if tt.Name != "" {
			appendWildcardDNS = true
		}
	}
	if appendWildcardDNS {
		hostRules = append(hostRules, fmt.Sprintf("*.%s", domain))
	}

	// By default we map to the placeholder service directly.
	// This would point to 'router' component if we wanted to use
	// this method for 0->1 case.
	serviceName := controller.GetElaK8SServiceName(route)

	path := v1beta1.HTTPIngressPath{
		Backend: v1beta1.IngressBackend{
			ServiceName: serviceName,
			ServicePort: intstr.IntOrString{Type: intstr.String, StrVal: "http"},
		},
	}
<<<<<<< HEAD
	rules := []v1beta1.IngressRule{v1beta1.IngressRule{
		Host: route.Status.Domain,
		IngressRuleValue: v1beta1.IngressRuleValue{
			HTTP: &v1beta1.HTTPIngressRuleValue{
				Paths: []v1beta1.HTTPIngressPath{path},
=======

	rules := []v1beta1.IngressRule{}
	for _, hostRule := range hostRules {
		rule := v1beta1.IngressRule{
			Host: hostRule,
			IngressRuleValue: v1beta1.IngressRuleValue{
				HTTP: &v1beta1.HTTPIngressRuleValue{
					Paths: []v1beta1.HTTPIngressPath{path},
				},
>>>>>>> Create routes for named traffic targets
			},
		}
		rules = append(rules, rule)
	}

	return &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controller.GetElaK8SIngressName(route),
			Namespace: route.Namespace,
			Annotations: map[string]string{
				"kubernetes.io/ingress.class": "istio",
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(route, controllerKind),
			},
		},
		Spec: v1beta1.IngressSpec{
			Rules: rules,
		},
	}
}
