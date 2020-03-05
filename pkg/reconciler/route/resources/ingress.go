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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/domains"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"
	"knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/pkg/reconciler/route/traffic"
)

// MakeIngressTLS creates IngressTLS to configure the ingress TLS.
func MakeIngressTLS(cert *v1alpha1.Certificate, hostNames []string) v1alpha1.IngressTLS {
	return v1alpha1.IngressTLS{
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
	tls []v1alpha1.IngressTLS,
	ingressClass string,
	acmeChallenges ...v1alpha1.HTTP01Challenge,
) (*v1alpha1.Ingress, error) {
	spec, err := MakeIngressSpec(ctx, r, tls, tc.Targets, tc.Visibility, acmeChallenges...)
	if err != nil {
		return nil, err
	}
	return &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.Ingress(r),
			Namespace: r.Namespace,
			Labels: kmeta.UnionMaps(r.ObjectMeta.Labels, map[string]string{
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
	tls []v1alpha1.IngressTLS,
	targets map[string]traffic.RevisionTargets,
	visibility map[string]netv1alpha1.IngressVisibility,
	acmeChallenges ...v1alpha1.HTTP01Challenge,
) (v1alpha1.IngressSpec, error) {
	// Domain should have been specified in route status
	// before calling this func.
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	// Sort the names to give things a deterministic ordering.
	sort.Strings(names)
	// The routes are matching rule based on domain name to traffic split targets.
	rules := make([]v1alpha1.IngressRule, 0, len(names))
	challengeHosts := getChallengeHosts(acmeChallenges)

	for _, name := range names {
		visibilities := []netv1alpha1.IngressVisibility{netv1alpha1.IngressVisibilityClusterLocal}
		// If this is a public target (or not being marked as cluster-local), we also make public rule.
		if v, ok := visibility[name]; !ok || v == netv1alpha1.IngressVisibilityExternalIP {
			visibilities = append(visibilities, netv1alpha1.IngressVisibilityExternalIP)
		}
		for _, visibility := range visibilities {
			domain, err := routeDomain(ctx, name, r, visibility)
			if err != nil {
				return v1alpha1.IngressSpec{}, err
			}
			rule := *makeIngressRule([]string{domain}, r.Namespace, visibility, targets[name])
			// If this is a public rule, we need to configure ACME challenge paths.
			if visibility == netv1alpha1.IngressVisibilityExternalIP {
				rule.HTTP.Paths = append(
					makeACMEIngressPaths(challengeHosts, []string{domain}), rule.HTTP.Paths...)
			}
			rules = append(rules, rule)
		}
	}

	return v1alpha1.IngressSpec{
		Rules: rules,
		TLS:   tls,
	}, nil
}

func getChallengeHosts(challenges []v1alpha1.HTTP01Challenge) map[string]v1alpha1.HTTP01Challenge {
	c := make(map[string]v1alpha1.HTTP01Challenge, len(challenges))

	for _, challenge := range challenges {
		c[challenge.URL.Host] = challenge
	}

	return c
}

func routeDomain(ctx context.Context, targetName string, r *servingv1.Route, visibility netv1alpha1.IngressVisibility) (string, error) {
	hostname, err := domains.HostnameFromTemplate(ctx, r.Name, targetName)
	if err != nil {
		return "", err
	}

	meta := r.ObjectMeta.DeepCopy()
	isClusterLocal := visibility == netv1alpha1.IngressVisibilityClusterLocal
	labels.SetVisibility(meta, isClusterLocal)

	return domains.DomainNameFromTemplate(ctx, *meta, hostname)
}

func makeACMEIngressPaths(challenges map[string]v1alpha1.HTTP01Challenge, domains []string) []v1alpha1.HTTPIngressPath {
	paths := make([]v1alpha1.HTTPIngressPath, 0, len(challenges))
	for _, domain := range domains {
		challenge, ok := challenges[domain]
		if !ok {
			continue
		}

		paths = append(paths, v1alpha1.HTTPIngressPath{
			Splits: []v1alpha1.IngressBackendSplit{{
				IngressBackend: v1alpha1.IngressBackend{
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

func makeIngressRule(domains []string, ns string, visibility netv1alpha1.IngressVisibility, targets traffic.RevisionTargets) *v1alpha1.IngressRule {
	// Optimistically allocate |targets| elements.
	splits := make([]v1alpha1.IngressBackendSplit, 0, len(targets))
	for _, t := range targets {
		if t.Percent == nil || *t.Percent == 0 {
			continue
		}

		splits = append(splits, v1alpha1.IngressBackendSplit{
			IngressBackend: v1alpha1.IngressBackend{
				ServiceNamespace: ns,
				ServiceName:      t.ServiceName,
				// Port on the public service must match port on the activator.
				// Otherwise, the serverless services can't guarantee seamless positive handoff.
				ServicePort: intstr.FromInt(int(networking.ServicePort(t.Protocol))),
			},
			Percent: int(*t.Percent),
			AppendHeaders: map[string]string{
				activator.RevisionHeaderName:      t.TrafficTarget.RevisionName,
				activator.RevisionHeaderNamespace: ns,
			},
		})
	}

	return &v1alpha1.IngressRule{
		Hosts:      domains,
		Visibility: visibility,
		HTTP: &v1alpha1.HTTPIngressRuleValue{
			Paths: []v1alpha1.HTTPIngressPath{{
				Splits: splits,
				// TODO(lichuqiang): #2201, plumbing to config timeout and retries.
			}},
		},
	}
}
