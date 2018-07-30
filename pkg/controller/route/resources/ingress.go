/*
Copyright 2018 The Knative Authors.

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
	"sort"

	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	revisionresources "github.com/knative/serving/pkg/controller/revision/resources"
	"github.com/knative/serving/pkg/controller/route/resources/names"
	"github.com/knative/serving/pkg/controller/route/traffic"
	// "github.com/knative/serving/pkg/system"
	extv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// MakeIngress creates an Ingress to set up routing rules.
func MakeIngress(u *v1alpha1.Route, tc *traffic.TrafficConfig) *extv1beta1.Ingress {
	// port := intstr.FromInt(int(revisionresources.ServicePort))
	return &extv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.VirtualService(u),
			Namespace:       u.Namespace,
			Labels:          map[string]string{"route": u.Name},
			OwnerReferences: []metav1.OwnerReference{*controller.NewControllerRef(u)},
		},
		Spec: makeIngressSpec(u, tc.Targets),
	}
}

func makeIngressSpec(u *v1alpha1.Route, targets map[string][]traffic.RevisionTarget) extv1beta1.IngressSpec {
	names := []string{}
	for name := range targets {
		names = append(names, name)
	}
	// Sort the names to give things a deterministic ordering.
	sort.Strings(names)

	spec := extv1beta1.IngressSpec{}
	// The routes are matching rule based on domain name to traffic split targets.
	for _, name := range names {
		rules := makeTrafficRules(
			getRouteDomains(name, u, u.Status.Domain),
			u.Namespace, targets[name])
		spec.Rules = append(spec.Rules, rules...)
	}
	return spec
}

func makeTrafficRules(domains []string, ns string, targets []traffic.RevisionTarget) []extv1beta1.IngressRule {
	value := makeIngressRuleValue(ns, targets)

	rules := make([]extv1beta1.IngressRule, 0, len(domains))
	for _, domain := range domains {
		rules = append(rules, extv1beta1.IngressRule{
			Host:             domain,
			IngressRuleValue: value,
		})
	}
	return rules
}

func makeIngressRuleValue(ns string, targets []traffic.RevisionTarget) extv1beta1.IngressRuleValue {
	active, inactive := groupInactiveTargets(targets)

	paths := []extv1beta1.HTTPIngressPath{}
	for _, t := range active {
		if t.Percent == 0 {
			// Elide 0% targets.
			continue
		}
		paths = append(paths, extv1beta1.HTTPIngressPath{
			Backend: extv1beta1.IngressBackend{
				ServiceName: controller.GetServingK8SServiceNameForObj(t.TrafficTarget.RevisionName),
				ServicePort: intstr.FromInt(int(revisionresources.ServicePort)),
			},
			// TODO(mattmoor): https://github.com/kubernetes/kubernetes/issues/25485
			// Weight: t.Percent,
		})
	}

	// See if we have >0% traffic going to inactive targets.
	totalInactivePercent := 0
	maxInactiveTarget := traffic.RevisionTarget{}
	for _, t := range inactive {
		totalInactivePercent += t.Percent
		if t.Percent >= maxInactiveTarget.Percent {
			maxInactiveTarget = t
		}
	}
	if totalInactivePercent != 0 {
		paths = append(paths, extv1beta1.HTTPIngressPath{
			// TODO(mattmoor): Consider using Path to encode the revision information.
			// e.g. /foo/bar to revision baz/blah would become /baz/blah/foo/bar and
			// the activator would strip the first two parts.
			Backend: extv1beta1.IngressBackend{
				ServiceName: activator.K8sServiceName,
				// TODO(mattmoor): Need a way to route to the common activator.
				// ServiceNamespace: system.Namespace,
				ServicePort: intstr.FromInt(int(revisionresources.ServicePort)),
			},
			// TODO(mattmoor): https://github.com/kubernetes/kubernetes/issues/25485
			// Weight: totalInactivePercent,
		})
		// TODO(mattmoor): Need a way to indicate the Revision to activate (see above)
		// r.AppendHeaders = map[string]string{
		// 	controller.GetRevisionHeaderName():      maxInactiveTarget.RevisionName,
		// 	controller.GetRevisionHeaderNamespace(): ns,
		// }
	}

	return extv1beta1.IngressRuleValue{
		HTTP: &extv1beta1.HTTPIngressRuleValue{
			Paths: paths,
		},
	}
}
