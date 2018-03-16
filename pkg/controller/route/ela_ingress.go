/*
Copyright 2017 The Kubernetes Authors.

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
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"

	v1beta1 "k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// MakeRouteIngress creates an ingress rule. This ingress rule targets
// Istio by using the simple placeholder service name. All the routing actually
// happens in the route rules.
func MakeRouteIngress(u *v1alpha1.Route, namespace string, domain string) *v1beta1.Ingress {
	// By default we map to the placeholder service directly.
	// This would point to 'router' component if we wanted to use
	// this method for 0->1 case.
	serviceName := controller.GetElaK8SServiceName(u)

	path := v1beta1.HTTPIngressPath{
		Backend: v1beta1.IngressBackend{
			ServiceName: serviceName,
			ServicePort: intstr.IntOrString{Type: intstr.String, StrVal: "http"},
		},
	}
	rules := []v1beta1.IngressRule{v1beta1.IngressRule{
		Host: domain,
		IngressRuleValue: v1beta1.IngressRuleValue{
			HTTP: &v1beta1.HTTPIngressRuleValue{
				Paths: []v1beta1.HTTPIngressPath{path},
			},
		},
	},
	}

	return &v1beta1.Ingress{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      controller.GetElaK8SIngressName(u),
			Namespace: namespace,
			Annotations: map[string]string{
				"kubernetes.io/ingress.class": "istio",
			},
		},
		Spec: v1beta1.IngressSpec{
			Rules: rules,
		},
	}
}
