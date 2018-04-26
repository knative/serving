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
	"github.com/josephburnett/k8sflag/pkg/k8sflag"

	v1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// MakeRouteIngress creates an ingress rule, owned by the provided v1alpha1.Route. This ingress rule
// targets Istio by using the simple placeholder service name. All the routing actually happens in
// the route rules.
func MakeRouteIngress(route *v1alpha1.Route, enableScaleToZero *k8sflag.BoolFlag) *v1beta1.Ingress {
	// We used to have a distinct service, but in the ela world, use the
	// name for serviceID too.

	// Construct hostnames the ingress accepts traffic for.
	hostRules := []string{
		route.Status.Domain,
		fmt.Sprintf("*.%s", route.Status.Domain),
	}

	// This would point to 'activator' component if enableActivatorExperiment is true.
	namespace := route.Namespace
	serviceName := controller.GetElaK8SServiceName(route)
	if enableScaleToZero.Get() {
		namespace = controller.GetElaK8SActivatorNamespace()
		serviceName = controller.GetElaK8SActivatorServiceName()
	}

	path := v1beta1.HTTPIngressPath{
		Backend: v1beta1.IngressBackend{
			ServiceName: serviceName,
			ServicePort: intstr.IntOrString{Type: intstr.String, StrVal: "http"},
		},
	}

	rules := []v1beta1.IngressRule{}
	for _, hostRule := range hostRules {
		rule := v1beta1.IngressRule{
			Host: hostRule,
			IngressRuleValue: v1beta1.IngressRuleValue{
				HTTP: &v1beta1.HTTPIngressRuleValue{
					Paths: []v1beta1.HTTPIngressPath{path},
				},
			},
		}
		rules = append(rules, rule)
	}

	return &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controller.GetElaK8SIngressName(route),
			Namespace: namespace,
			Annotations: map[string]string{
				"kubernetes.io/ingress.class": "istio",
			},
			OwnerReferences: []metav1.OwnerReference{
				*controller.NewRouteControllerRef(route),
			},
		},
		Spec: v1beta1.IngressSpec{
			Rules: rules,
		},
	}
}
