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

package resources

import (
	"fmt"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"

	istiov1alpha1 "github.com/knative/pkg/apis/istio/common/v1alpha1"
	"github.com/knative/pkg/apis/istio/v1alpha3"
	"github.com/knative/pkg/kmeta"
	"github.com/knative/pkg/system"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler/ingress/resources/names"
	"github.com/knative/serving/pkg/resources"
)

// VirtualServiceNamespace return namespace of VirtualService based on parent
// If the parent is ClusterIngress, return systme namespace for backwoard compatibility.
// Otherwise, the namespace of the parent.
func VirtualServiceNamespace(ia v1alpha1.IngressAccessor) string {
	if len(ia.GetObjectMeta().GetNamespace()) == 0 {
		return system.Namespace()
	} else {
		return ia.GetObjectMeta().GetNamespace()
	}
}

// MakeVirtualService creates Istio VirtualService as network
// programming for Istio Gateways other than 'mesh'
func MakeVirtualService(ia v1alpha1.IngressAccessor, gateways []string) *v1alpha3.VirtualService {
	vs := makeVirtualService(ia, gateways)

	// Populate the Ingress labels.
	if vs.Labels == nil {
		vs.Labels = make(map[string]string)
	}

	populateLabels(vs.Labels, ia)
	return vs
}

// This method populates Virtual Service labels
func populateLabels(vsLabels map[string]string, ia v1alpha1.IngressAccessor) {
	ingressLabels := ia.GetObjectMeta().GetLabels()
	vsLabels[serving.RouteLabelKey] = ingressLabels[serving.RouteLabelKey]
	vsLabels[serving.RouteNamespaceLabelKey] = ingressLabels[serving.RouteNamespaceLabelKey]

	if len(ia.GetObjectMeta().GetNamespace()) == 0 {
		vsLabels[networking.ClusterIngressLabelKey] = ia.GetObjectMeta().GetName()
	} else {
		vsLabels[networking.IngressNamespaceLabelKey] = ia.GetObjectMeta().GetNamespace()
		vsLabels[networking.IngressLabelKey] = ia.GetObjectMeta().GetName()
	}
}

// makeVirtualService creates Istio VirtualService as network
// programming for Istio Gateways other than 'mesh'.
func makeVirtualService(ia v1alpha1.IngressAccessor, gateways []string) *v1alpha3.VirtualService {
	return &v1alpha3.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.IngressVirtualService(ia),
			Namespace:       VirtualServiceNamespace(ia),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ia)},
			Annotations:     ia.GetObjectMeta().GetAnnotations(),
		},
		Spec: *makeVirtualServiceSpec(ia.GetSpec(), gateways, expandedHosts(getHosts(ia.GetSpec()))),
	}
}

// MakeMeshVirtualService creates Istio VirtualService as network
// programming for Istio network mesh.
func MakeMeshVirtualService(ia v1alpha1.IngressAccessor) *v1alpha3.VirtualService {
	vs := makeMeshVirtualService(ia)
	// Populate the Ingress labels.
	vs.Labels = getMeshLabels(ia)

	return vs
}

func getMeshLabels(ia v1alpha1.IngressAccessor) map[string]string {
	labels := make(map[string]string)

	if len(ia.GetObjectMeta().GetNamespace()) == 0 {
		labels[networking.ClusterIngressLabelKey] = ia.GetObjectMeta().GetName()
	} else {
		labels[networking.IngressLabelKey] = ia.GetObjectMeta().GetName()
		labels[networking.IngressNamespaceLabelKey] = ia.GetObjectMeta().GetNamespace()
	}

	return resources.UnionMaps(resources.FilterMap(ia.GetObjectMeta().GetLabels(), func(k string) bool {
		return k != serving.RouteLabelKey && k != serving.RouteNamespaceLabelKey
	}), labels)
}

func makeMeshVirtualService(ia v1alpha1.IngressAccessor) *v1alpha3.VirtualService {
	vs := &v1alpha3.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.IngressMeshVirtualService(ia),
			Namespace:       VirtualServiceNamespace(ia),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ia)},
			Annotations:     ia.GetObjectMeta().GetAnnotations(),
		},
		Spec: *makeVirtualServiceSpec(ia.GetSpec(), []string{"mesh"}, retainLocals(getHosts(ia.GetSpec()))),
	}
	return vs
}

// MakeVirtualServices creates Istio VirtualServices as network programming.
// These VirtualService specifies which Gateways and Hosts that it applies to,
// as well as the routing rules.
func MakeVirtualServices(ia v1alpha1.IngressAccessor, gateways []string) []*v1alpha3.VirtualService {
	vss := []*v1alpha3.VirtualService{MakeMeshVirtualService(ia)}
	if len(gateways) > 0 {
		vss = append(vss, MakeVirtualService(ia, gateways))
	}
	return vss
}

func makeVirtualServiceSpec(ingressSpec *v1alpha1.IngressSpec, gateways []string, hosts []string) *v1alpha3.VirtualServiceSpec {
	spec := v1alpha3.VirtualServiceSpec{
		// add system namespace to gateway as gateway may not be in the same namespace as version service
		Gateways: addSystemNamespaceTo(gateways),
		Hosts:    hosts,
	}
	for _, rule := range ingressSpec.Rules {
		for _, p := range rule.HTTP.Paths {
			hosts := intersect(rule.Hosts, hosts)
			if len(hosts) != 0 {
				spec.HTTP = append(spec.HTTP, *makeVirtualServiceRoute(hosts, &p))
			}
		}
	}
	return &spec
}

func addSystemNamespaceTo(gateways []string) []string {
	namespacedGateways := make([]string, len(gateways))
	for i, gateway := range gateways {
		namespacedGateways[i] = system.Namespace() + "/" + gateway
	}
	return namespacedGateways
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

func makeVirtualServiceRoute(hosts []string, http *v1alpha1.HTTPIngressPath) *v1alpha3.HTTPRoute {
	matches := []v1alpha3.HTTPMatchRequest{}
	for _, host := range expandedHosts(hosts) {
		matches = append(matches, makeMatch(host, http.Path))
	}
	weights := []v1alpha3.HTTPRouteDestination{}
	for _, split := range http.Splits {

		var h *v1alpha3.Headers
		// TODO(mattmoor): Switch to Headers when we can have a hard
		// dependency on 1.1, but 1.0.x rejects the unknown fields.
		// if len(split.AppendHeaders) > 0 {
		// 	h = &v1alpha3.Headers{
		// 		Request: &v1alpha3.HeaderOperations{
		// 			Add: split.AppendHeaders,
		// 		},
		// 	}
		// }

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
	// TODO(mattmoor): Switch to Headers when we can have a hard
	// dependency on 1.1, but 1.0.x rejects the unknown fields.
	// if len(http.AppendHeaders) > 0 {
	// 	h = &v1alpha3.Headers{
	// 		Request: &v1alpha3.HeaderOperations{
	// 			Add: http.AppendHeaders,
	// 		},
	// 	}
	// }

	return &v1alpha3.HTTPRoute{
		Match:   matches,
		Route:   weights,
		Timeout: http.Timeout.Duration.String(),
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      http.Retries.Attempts,
			PerTryTimeout: http.Retries.PerTryTimeout.Duration.String(),
		},
		// TODO(mattmoor): Remove AppendHeaders when 1.1 is a hard dependency.
		// AppendHeaders is deprecated in Istio 1.1 in favor of Headers,
		// however, 1.0.x doesn't support Headers.
		DeprecatedAppendHeaders: http.AppendHeaders,
		Headers:                 h,
		WebsocketUpgrade:        true,
	}
}

func dedup(hosts []string) []string {
	return sets.NewString(hosts...).List()
}

func retainLocals(hosts []string) []string {
	localSvcSuffix := ".svc." + network.GetClusterDomainName()
	retained := []string{}
	for _, h := range hosts {
		if strings.HasSuffix(h, localSvcSuffix) {
			retained = append(retained, h)
		}
	}
	return retained
}

func expandedHosts(hosts []string) []string {
	expanded := []string{}
	allowedSuffixes := []string{
		"",
		"." + network.GetClusterDomainName(),
		".svc." + network.GetClusterDomainName(),
	}
	for _, h := range hosts {
		for _, suffix := range allowedSuffixes {
			if strings.HasSuffix(h, suffix) {
				expanded = append(expanded, strings.TrimSuffix(h, suffix))
			}
		}
	}
	return dedup(expanded)
}

func makeMatch(host string, pathRegExp string) v1alpha3.HTTPMatchRequest {
	match := v1alpha3.HTTPMatchRequest{
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
func hostRegExp(host string) string {
	return fmt.Sprintf("^%s%s$", regexp.QuoteMeta(host), portMatch)
}

func getHosts(spec *v1alpha1.IngressSpec) []string {
	hosts := make([]string, 0, len(spec.Rules))
	for _, rule := range spec.Rules {
		hosts = append(hosts, rule.Hosts...)
	}
	return dedup(hosts)
}

func intersect(h1, h2 []string) []string {
	return sets.NewString(h1...).Intersection(sets.NewString(h2...)).List()
}
