/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"context"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"

	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	ingress "knative.dev/networking/pkg/ingress"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/activator"
	apiConfig "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/domains"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"
	"knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/pkg/reconciler/route/traffic"
)

// MakeIngressTLS creates IngressTLS to configure the ingress TLS.
func MakeIngressTLS(cert *netv1alpha1.Certificate, hostNames []string) netv1alpha1.IngressTLS {
	return netv1alpha1.IngressTLS{
		Hosts:           hostNames,
		SecretName:      cert.Spec.SecretName,
		SecretNamespace: cert.Namespace,
	}
}

// MakeIngress creates Ingress to set up routing rules. Such Ingress specifies
// which Hosts that it applies to, as well as the routing rules.
func MakeIngress(
	ctx context.Context,
	r *servingv1.Route,
	tc *traffic.Config,
	tls []netv1alpha1.IngressTLS,
	ingressClass string,
	acmeChallenges ...netv1alpha1.HTTP01Challenge,
) (*netv1alpha1.Ingress, error) {
	spec, err := MakeIngressSpec(ctx, r, tls, tc.Targets, tc.Visibility, acmeChallenges...)
	if err != nil {
		return nil, err
	}
	return &netv1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.Ingress(r),
			Namespace: r.Namespace,
			Labels: kmeta.UnionMaps(r.Labels, map[string]string{
				serving.RouteLabelKey:          r.Name,
				serving.RouteNamespaceLabelKey: r.Namespace,
			}),
			Annotations: kmeta.FilterMap(kmeta.UnionMaps(map[string]string{
				networking.IngressClassAnnotationKey: ingressClass,
			}, r.GetAnnotations()), func(key string) bool {
				return key == corev1.LastAppliedConfigAnnotation
			}),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(r)},
		},
		Spec: spec,
	}, nil
}

// MakeIngressSpec creates a new IngressSpec
func MakeIngressSpec(
	ctx context.Context,
	r *servingv1.Route,
	tls []netv1alpha1.IngressTLS,
	targets map[string]traffic.RevisionTargets,
	visibility map[string]netv1alpha1.IngressVisibility,
	acmeChallenges ...netv1alpha1.HTTP01Challenge,
) (netv1alpha1.IngressSpec, error) {
	// Domain should have been specified in route status
	// before calling this func.
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	// Sort the names to give things a deterministic ordering.
	sort.Strings(names)
	// The routes are matching rule based on domain name to traffic split targets.
	rules := make([]netv1alpha1.IngressRule, 0, len(names))
	challengeHosts := getChallengeHosts(acmeChallenges)

	featuresConfig := config.FromContextOrDefaults(ctx).Features

	for _, name := range names {
		visibilities := []netv1alpha1.IngressVisibility{netv1alpha1.IngressVisibilityClusterLocal}
		// If this is a public target (or not being marked as cluster-local), we also make public rule.
		if v, ok := visibility[name]; !ok || v == netv1alpha1.IngressVisibilityExternalIP {
			visibilities = append(visibilities, netv1alpha1.IngressVisibilityExternalIP)
		}
		for _, visibility := range visibilities {
			domains, err := routeDomain(ctx, name, r, visibility)
			if err != nil {
				return netv1alpha1.IngressSpec{}, err
			}
			rule := makeIngressRule(ctx, domains, r.Namespace, visibility, targets[name])
			if featuresConfig.TagHeaderBasedRouting == apiConfig.Enabled {
				if rule.HTTP.Paths[0].AppendHeaders == nil {
					rule.HTTP.Paths[0].AppendHeaders = make(map[string]string)
				}

				if name == traffic.DefaultTarget {
					// To provide a information if a request is routed via the "default route" or not,
					// the header "Knative-Serving-Default-Route: true" is appended here.
					// If the header has "true" and there is a "Knative-Serving-Tag" header,
					// then the request is having the undefined tag header, which will be observed in queue-proxy.
					rule.HTTP.Paths[0].AppendHeaders[network.DefaultRouteHeaderName] = "true"
					// Add ingress paths for a request with the tag header.
					// If a request has one of the `names`(tag name) except the default path,
					// the request will be routed via one of the ingress paths, corresponding to the tag name.
					rule.HTTP.Paths = append(
						makeTagBasedRoutingIngressPaths(ctx, r.Namespace, targets, names), rule.HTTP.Paths...)
				} else {
					// If a request is routed by a tag-attached hostname instead of the tag header,
					// the request may not have the tag header "Knative-Serving-Tag",
					// even though the ingress path used in the case is also originated
					// from the same Knative route with the ingress path for the tag based routing.
					//
					// To prevent such inconsistency,
					// the tag header is appended with the tag corresponding to the tag-attached hostname
					rule.HTTP.Paths[0].AppendHeaders[network.TagHeaderName] = name
				}
			}
			// If this is a public rule, we need to configure ACME challenge paths.
			if visibility == netv1alpha1.IngressVisibilityExternalIP {
				rule.HTTP.Paths = append(
					makeACMEIngressPaths(challengeHosts, domains), rule.HTTP.Paths...)
			}
			rules = append(rules, rule)
		}
	}

	return netv1alpha1.IngressSpec{
		Rules: rules,
		TLS:   tls,
	}, nil
}

func getChallengeHosts(challenges []netv1alpha1.HTTP01Challenge) map[string]netv1alpha1.HTTP01Challenge {
	c := make(map[string]netv1alpha1.HTTP01Challenge, len(challenges))

	for _, challenge := range challenges {
		c[challenge.URL.Host] = challenge
	}

	return c
}

func routeDomain(ctx context.Context, targetName string, r *servingv1.Route, visibility netv1alpha1.IngressVisibility) ([]string, error) {
	hostname, err := domains.HostnameFromTemplate(ctx, r.Name, targetName)
	if err != nil {
		return []string{}, err
	}

	meta := r.ObjectMeta.DeepCopy()
	isClusterLocal := visibility == netv1alpha1.IngressVisibilityClusterLocal
	labels.SetVisibility(meta, isClusterLocal)

	domain, err := domains.DomainNameFromTemplate(ctx, *meta, hostname)
	if err != nil {
		return []string{}, err
	}
	domains := []string{domain}
	if isClusterLocal {
		domains = ingress.ExpandedHosts(sets.NewString(domains...)).List()
	}
	return domains, err
}

func makeACMEIngressPaths(challenges map[string]netv1alpha1.HTTP01Challenge, domains []string) []netv1alpha1.HTTPIngressPath {
	paths := make([]netv1alpha1.HTTPIngressPath, 0, len(challenges))
	for _, domain := range domains {
		challenge, ok := challenges[domain]
		if !ok {
			continue
		}

		paths = append(paths, netv1alpha1.HTTPIngressPath{
			Splits: []netv1alpha1.IngressBackendSplit{{
				IngressBackend: netv1alpha1.IngressBackend{
					ServiceNamespace: challenge.ServiceNamespace,
					ServiceName:      challenge.ServiceName,
					ServicePort:      challenge.ServicePort,
				},
				Percent: 100,
			}},
			Path: challenge.URL.Path,
		})
	}
	return paths
}

func makeIngressRule(ctx context.Context,
	domains []string, ns string,
	visibility netv1alpha1.IngressVisibility, targets traffic.RevisionTargets) netv1alpha1.IngressRule {
	return netv1alpha1.IngressRule{
		Hosts:      domains,
		Visibility: visibility,
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{
				*makeBaseIngressPath(ctx, ns, targets),
			},
		},
	}
}

func makeTagBasedRoutingIngressPaths(
	ctx context.Context, ns string, targets map[string]traffic.RevisionTargets, names []string) []netv1alpha1.HTTPIngressPath {
	paths := make([]netv1alpha1.HTTPIngressPath, 0, len(names))

	for _, name := range names {
		if name != traffic.DefaultTarget {
			path := makeBaseIngressPath(ctx, ns, targets[name])
			path.Headers = map[string]netv1alpha1.HeaderMatch{network.TagHeaderName: {Exact: name}}
			paths = append(paths, *path)
		}
	}

	return paths
}

func makeBaseIngressPath(
	ctx context.Context, ns string, targets traffic.RevisionTargets) *netv1alpha1.HTTPIngressPath {
	// Optimistically allocate |targets| elements.
	splits := make([]netv1alpha1.IngressBackendSplit, 0, len(targets))
	for _, t := range targets {
		if t.Percent == nil || *t.Percent == 0 {
			continue
		}

		splits = append(splits, netv1alpha1.IngressBackendSplit{
			IngressBackend: netv1alpha1.IngressBackend{
				ServiceNamespace: ns,
				ServiceName:      t.ServiceName,
				// Port on the public service must match port on the activator.
				// Otherwise, the serverless services can't guarantee seamless positive handoff.
				ServicePort: intstr.FromInt(networking.ServicePort(t.Protocol)),
			},
			Percent: int(*t.Percent),
			AppendHeaders: map[string]string{
				activator.RevisionHeaderName:      t.TrafficTarget.RevisionName,
				activator.RevisionHeaderNamespace: ns,
			},
		})
	}

	return &netv1alpha1.HTTPIngressPath{
		Splits: splits,
		Timeout: &metav1.Duration{
			Duration: ingressTimeout(ctx),
		},
	}
}

// Before https://github.com/knative/networking/pull/64 we used a
// default value in the KIngress timeout settings. However, that
// does not work well with gRPC streaming timeout, so we stop the
// defaulting going forward.
//
// However, that is a breaking change for KIngress
// implementations. It broke Contour, and breaks Gloo.
//
// In order to give the Ingress implementers to have time to
// implement this `no timeout` behavior we will specify a high
// timeout value in Route controller in the mean time.
func ingressTimeout(ctx context.Context) time.Duration {
	// We want to set the ingress timeout to a really long timeout to
	// helps with issues like
	// https://github.com/knative/serving/issues/7350#issuecomment-669278261
	longTimeout := time.Hour * 48

	// However, if the MaxRevisionTimeout is longer, we should still honor that.
	if apiConfig.FromContext(ctx) != nil && apiConfig.FromContext(ctx).Defaults != nil {
		maxRevisionTimeout := time.Duration(
			apiConfig.FromContext(ctx).Defaults.MaxRevisionTimeoutSeconds) * time.Second
		if maxRevisionTimeout > longTimeout {
			return maxRevisionTimeout
		}
	}
	return longTimeout
}
