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
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"

	istiov1alpha1 "knative.dev/pkg/apis/istio/common/v1alpha1"
	"knative.dev/pkg/apis/istio/v1alpha3"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler/ingress/resources/names"
	"knative.dev/serving/pkg/resources"
)

// VirtualServiceNamespace gives the namespace of the child
// VirtualServices for a given ClusterIngress.
func VirtualServiceNamespace(ia v1alpha1.IngressAccessor) string {
	if len(ia.GetNamespace()) == 0 {
		return system.Namespace()
	}
	return ia.GetNamespace()
}

// MakeIngressVirtualService creates Istio VirtualService as network
// programming for Istio Gateways other than 'mesh'.
func MakeIngressVirtualService(ia v1alpha1.IngressAccessor, gateways map[v1alpha1.IngressVisibility]sets.String) *v1alpha3.VirtualService {
	vs := &v1alpha3.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.IngressVirtualService(ia),
			Namespace:       VirtualServiceNamespace(ia),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ia)},
			Annotations:     ia.GetAnnotations(),
		},
		Spec: *makeVirtualServiceSpec(ia, gateways, expandedHosts(getHosts(ia))),
	}

	// Populate the ClusterIngress labels.
	if vs.Labels == nil {
		vs.Labels = make(map[string]string)
	}

	if len(ia.GetNamespace()) == 0 {
		vs.Labels[networking.ClusterIngressLabelKey] = ia.GetName()
	}

	ingressLabels := ia.GetLabels()
	vs.Labels[serving.RouteLabelKey] = ingressLabels[serving.RouteLabelKey]
	vs.Labels[serving.RouteNamespaceLabelKey] = ingressLabels[serving.RouteNamespaceLabelKey]
	return vs
}

// MakeMeshVirtualService creates a mesh Virtual Service
func MakeMeshVirtualService(ia v1alpha1.IngressAccessor) *v1alpha3.VirtualService {
	vs := &v1alpha3.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.MeshVirtualService(ia),
			Namespace:       VirtualServiceNamespace(ia),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ia)},
			Annotations:     ia.GetAnnotations(),
		},
		Spec: *makeVirtualServiceSpec(ia, map[v1alpha1.IngressVisibility]sets.String{
			v1alpha1.IngressVisibilityExternalIP:   sets.NewString("mesh"),
			v1alpha1.IngressVisibilityClusterLocal: sets.NewString("mesh"),
		}, keepLocalHostnames(getHosts(ia))),
	}
	// Populate the ClusterIngress labels.

	vs.Labels = resources.FilterMap(ia.GetLabels(), func(k string) bool {
		return k != serving.RouteLabelKey && k != serving.RouteNamespaceLabelKey
	})

	if len(ia.GetNamespace()) == 0 {
		vs.Labels[networking.ClusterIngressLabelKey] = ia.GetName()
	}

	return vs
}

// MakeVirtualServices creates a mesh virtualservice and a virtual service for each gateway
func MakeVirtualServices(ia v1alpha1.IngressAccessor, gateways map[v1alpha1.IngressVisibility]sets.String) []*v1alpha3.VirtualService {
	vss := []*v1alpha3.VirtualService{MakeMeshVirtualService(ia)}

	requiredGatewayCount := 0
	if len(getPublicIngressRules(ia)) > 0 {
		requiredGatewayCount += gateways[v1alpha1.IngressVisibilityExternalIP].Len()
	}

	if len(getClusterLocalIngressRules(ia)) > 0 {
		requiredGatewayCount += gateways[v1alpha1.IngressVisibilityClusterLocal].Len()
	}

	if requiredGatewayCount > 0 {
		vss = append(vss, MakeIngressVirtualService(ia, gateways))
	}
	return vss
}

func makeVirtualServiceSpec(ia v1alpha1.IngressAccessor, gateways map[v1alpha1.IngressVisibility]sets.String, hosts sets.String) *v1alpha3.VirtualServiceSpec {
	gw := sets.String{}.Union(gateways[v1alpha1.IngressVisibilityClusterLocal]).Union(gateways[v1alpha1.IngressVisibilityExternalIP])
	spec := v1alpha3.VirtualServiceSpec{
		Gateways: gw.List(),
		Hosts:    hosts.List(),
	}
	for _, rule := range ia.GetSpec().Rules {
		for _, p := range rule.HTTP.Paths {
			hosts := hosts.Intersection(sets.NewString(rule.Hosts...))
			if hosts.Len() != 0 {
				spec.HTTP = append(spec.HTTP, *makeVirtualServiceRoute(hosts, &p, gateways[rule.Visibility]))
			}
		}
	}
	return &spec
}

func makePortSelector(ios intstr.IntOrString) v1alpha3.PortSelector {
	if ios.Type == intstr.Int {
		return v1alpha3.PortSelector{
			Number: uint32(ios.IntValue()),
		}
	}
	return v1alpha3.PortSelector{
		Name: ios.String(),
	}
}

func makeVirtualServiceRoute(hosts sets.String, http *v1alpha1.HTTPIngressPath, gateways sets.String) *v1alpha3.HTTPRoute {
	matches := []v1alpha3.HTTPMatchRequest{}
	for _, host := range hosts.List() {
		matches = append(matches, makeMatch(host, http.Path, gateways))
	}
	weights := []v1alpha3.HTTPRouteDestination{}
	for _, split := range http.Splits {

		var h *v1alpha3.Headers

		if len(split.AppendHeaders) > 0 {
			h = &v1alpha3.Headers{
				Request: &v1alpha3.HeaderOperations{
					Add: split.AppendHeaders,
				},
			}
		}

		weights = append(weights, v1alpha3.HTTPRouteDestination{
			Destination: v1alpha3.Destination{
				Host: network.GetServiceHostname(
					split.ServiceName, split.ServiceNamespace),
				Port: makePortSelector(split.ServicePort),
			},
			Weight:  split.Percent,
			Headers: h,
		})
	}

	var h *v1alpha3.Headers
	if len(http.AppendHeaders) > 0 {
		h = &v1alpha3.Headers{
			Request: &v1alpha3.HeaderOperations{
				Add: http.AppendHeaders,
			},
		}
	}

	return &v1alpha3.HTTPRoute{
		Match:   matches,
		Route:   weights,
		Timeout: http.Timeout.Duration.String(),
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      http.Retries.Attempts,
			PerTryTimeout: http.Retries.PerTryTimeout.Duration.String(),
		},
		Headers:          h,
		WebsocketUpgrade: true,
	}
}

func keepLocalHostnames(hosts sets.String) sets.String {
	localSvcSuffix := ".svc." + network.GetClusterDomainName()
	retained := sets.NewString()
	for _, h := range hosts.List() {
		if strings.HasSuffix(h, localSvcSuffix) {
			retained.Insert(h)
		}
	}
	return retained
}

func expandedHosts(hosts sets.String) sets.String {
	expanded := sets.NewString()
	allowedSuffixes := []string{
		"",
		"." + network.GetClusterDomainName(),
		".svc." + network.GetClusterDomainName(),
	}
	for _, h := range hosts.List() {
		for _, suffix := range allowedSuffixes {
			if strings.HasSuffix(h, suffix) {
				expanded.Insert(strings.TrimSuffix(h, suffix))
			}
		}
	}
	return expanded
}

func makeMatch(host string, pathRegExp string, gateways sets.String) v1alpha3.HTTPMatchRequest {
	match := v1alpha3.HTTPMatchRequest{
		Gateways: gateways.List(),
		Authority: &istiov1alpha1.StringMatch{
			Regex: hostRegExp(host),
		},
	}
	// Empty pathRegExp is considered match all path. We only need to
	// consider pathRegExp when it's non-empty.
	if pathRegExp != "" {
		match.URI = &istiov1alpha1.StringMatch{
			Regex: pathRegExp,
		}
	}
	return match
}

// Should only match 1..65535, but for simplicity it matches 0-99999.
const portMatch = `(?::\d{1,5})?`

// hostRegExp returns an ECMAScript regular expression to match either host or host:<any port>
// for clusterLocalHost, we will also match the prefixes.
func hostRegExp(host string) string {
	localDomainSuffix := ".svc." + network.GetClusterDomainName()
	if !strings.HasSuffix(host, localDomainSuffix) {
		return exact(regexp.QuoteMeta(host) + portMatch)
	}
	prefix := regexp.QuoteMeta(strings.TrimSuffix(host, localDomainSuffix))
	clusterSuffix := regexp.QuoteMeta("." + network.GetClusterDomainName())
	svcSuffix := regexp.QuoteMeta(".svc")
	return exact(prefix + optional(svcSuffix+optional(clusterSuffix)) + portMatch)
}

func exact(regexp string) string {
	return "^" + regexp + "$"
}

func optional(regexp string) string {
	return "(" + regexp + ")?"
}
func getHosts(ia v1alpha1.IngressAccessor) sets.String {
	hosts := sets.NewString()
	for _, rule := range ia.GetSpec().Rules {
		hosts.Insert(rule.Hosts...)
	}
	return hosts
}

func intersect(h1, h2 []string) []string {
	return sets.NewString(h1...).Intersection(sets.NewString(h2...)).List()
}

func getClusterLocalIngressRules(ci v1alpha1.IngressAccessor) []v1alpha1.IngressRule {
	var result []v1alpha1.IngressRule
	for _, rule := range ci.GetSpec().Rules {
		if rule.Visibility == v1alpha1.IngressVisibilityClusterLocal {
			result = append(result, rule)
		}
	}

	return result
}

func getPublicIngressRules(ci v1alpha1.IngressAccessor) []v1alpha1.IngressRule {
	var result []v1alpha1.IngressRule
	for _, rule := range ci.GetSpec().Rules {
		if rule.Visibility == v1alpha1.IngressVisibilityExternalIP || rule.Visibility == "" {
			result = append(result, rule)
		}
	}

	return result
}
