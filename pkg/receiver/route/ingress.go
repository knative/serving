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

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"

	corev1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// SyncIngress implements ingress.Receiver
func (c *Receiver) SyncIngress(ingress *v1beta1.Ingress) error {
	// If ingress isn't owned by a route, no route update is required.
	routeName := controller.LookupOwningRouteName(ingress.OwnerReferences)
	if routeName == "" {
		return nil
	}
	if len(ingress.Status.LoadBalancer.Ingress) == 0 {
		// Route isn't ready if having no load-balancer ingress.
		return nil
	}
	for _, i := range ingress.Status.LoadBalancer.Ingress {
		if i.IP == "" {
			// Route isn't ready if some load balancer ingress doesn't have an IP.
			return nil
		}
	}
	ns := ingress.Namespace
	routeClient := c.ElaClientSet.ServingV1alpha1().Routes(ns)
	route, err := routeClient.Get(routeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if route.Status.IsReady() {
		return nil
	}
	// Mark route as ready.
	route.Status.SetCondition(&v1alpha1.RouteCondition{
		Type:   v1alpha1.RouteConditionReady,
		Status: corev1.ConditionTrue,
	})

	if _, err := routeClient.Update(route); err != nil {
		return err
	}
	return nil
}

// MakeRouteIngress creates an ingress rule, owned by the provided v1alpha1.Route. This ingress rule
// targets Istio by using the simple placeholder service name. All the routing actually happens in
// the route rules.
func MakeRouteIngress(route *v1alpha1.Route) *v1beta1.Ingress {
	// We used to have a distinct service, but in the ela world, use the
	// name for serviceID too.

	// Construct hostnames the ingress accepts traffic for.
	hostRules := []string{
		route.Status.Domain,
		fmt.Sprintf("*.%s", route.Status.Domain),
	}

	path := v1beta1.HTTPIngressPath{
		Backend: v1beta1.IngressBackend{
			ServiceName: controller.GetElaK8SServiceName(route),
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
			Namespace: route.Namespace,
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
