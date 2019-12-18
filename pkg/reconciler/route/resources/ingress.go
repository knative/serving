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
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	servingv1alpha1 "knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/reconciler/route/domains"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"
	"knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/pkg/reconciler/route/traffic"
	"knative.dev/serving/pkg/resources"
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
	r *servingv1alpha1.Route,
	tc *traffic.Config,
	tls []v1alpha1.IngressTLS,
	clusterLocalServices sets.String,
	ingressClass string,
	acmeChallenges ...v1alpha1.HTTP01Challenge,
) (*v1alpha1.Ingress, error) {
	spec, err := MakeIngressSpec(ctx, r, tls, clusterLocalServices, tc.Targets, acmeChallenges...)
	if err != nil {
		return nil, err
	}
	return &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.Ingress(r),
			Namespace: r.Namespace,
			Labels: resources.UnionMaps(r.ObjectMeta.Labels, map[string]string{
				serving.RouteLabelKey:          r.Name,
				serving.RouteNamespaceLabelKey: r.Namespace,
			}),
			Annotations: resources.FilterMap(resources.UnionMaps(map[string]string{
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
	r *servingv1alpha1.Route,
	tls []v1alpha1.IngressTLS,
	clusterLocalServices sets.String,
	targets map[string]traffic.RevisionTargets,
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
		serviceDomain, err := domains.HostnameFromTemplate(ctx, r.Name, name)
		if err != nil {
			return v1alpha1.IngressSpec{}, err
		}

		isClusterLocal := clusterLocalServices.Has(serviceDomain)

		routeDomains, err := routeDomains(ctx, name, r, isClusterLocal)
		if err != nil {
			return v1alpha1.IngressSpec{}, err
		}

		rule := *makeIngressRule(routeDomains, r.Namespace, isClusterLocal, targets[name])
		rule.HTTP.Paths = append(makeACMEIngressPaths(challengeHosts, routeDomains), rule.HTTP.Paths...)

		rules = append(rules, rule)
	}

	defaultDomain, err := domains.HostnameFromTemplate(ctx, r.Name, "")
	if err != nil {
		return v1alpha1.IngressSpec{}, err
	}

	visibility := v1alpha1.IngressVisibilityExternalIP
	if clusterLocalServices.Has(defaultDomain) {
		visibility = v1alpha1.IngressVisibilityClusterLocal
	}

	return v1alpha1.IngressSpec{
		Rules:      rules,
		Visibility: visibility,
		TLS:        tls,
	}, nil
}

func getChallengeHosts(challenges []v1alpha1.HTTP01Challenge) map[string]v1alpha1.HTTP01Challenge {
	c := make(map[string]v1alpha1.HTTP01Challenge, len(challenges))

	for _, challenge := range challenges {
		c[challenge.URL.Host] = challenge
	}

	return c
}

func routeDomains(ctx context.Context, targetName string, r *servingv1alpha1.Route, isClusterLocal bool) ([]string, error) {
	hostname, err := domains.HostnameFromTemplate(ctx, r.Name, targetName)
	if err != nil {
		return nil, err
	}

	meta := r.ObjectMeta.DeepCopy()
	labels.SetVisibility(meta, true)
	clusterLocalName, err := domains.DomainNameFromTemplate(ctx, *meta, hostname)
	if err != nil {
		return nil, err
	}
	ruleDomains := []string{clusterLocalName}

	if !isClusterLocal {
		labels.SetVisibility(meta, false)
		fullName, err := domains.DomainNameFromTemplate(ctx, *meta, hostname)
		if err != nil {
			return nil, err
		}

		if fullName != clusterLocalName {
			ruleDomains = append(ruleDomains, fullName)
		}
	}

	return ruleDomains, nil
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

func makeIngressRule(domains []string, ns string, isClusterLocal bool, targets traffic.RevisionTargets) *v1alpha1.IngressRule {
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

	visibility := v1alpha1.IngressVisibilityExternalIP
	if isClusterLocal {
		visibility = v1alpha1.IngressVisibilityClusterLocal
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
