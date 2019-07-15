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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
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

// MakeClusterIngress creates ClusterIngress to set up routing rules. Such ClusterIngress specifies
// which Hosts that it applies to, as well as the routing rules.
func MakeClusterIngress(
	ctx context.Context,
	r *servingv1alpha1.Route,
	tc *traffic.Config,
	tls []v1alpha1.IngressTLS,
	clusterLocalServices sets.String,
	ingressClass string,
) (v1alpha1.IngressAccessor, error) {
	spec, err := MakeIngressSpec(ctx, r, tls, clusterLocalServices, tc.Targets)
	if err != nil {
		return nil, err
	}
	return &v1alpha1.ClusterIngress{
		ObjectMeta: metav1.ObjectMeta{
			// As ClusterIngress resource is cluster-scoped,
			// here we use GenerateName to avoid conflict.
			Name: names.ClusterIngress(r),
			Labels: map[string]string{
				serving.RouteLabelKey:          r.Name,
				serving.RouteNamespaceLabelKey: r.Namespace,
			},
			Annotations: resources.UnionMaps(map[string]string{
				networking.IngressClassAnnotationKey: ingressClass,
			}, r.ObjectMeta.Annotations),
		},
		Spec: spec,
	}, nil
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
) (v1alpha1.IngressAccessor, error) {
	spec, err := MakeIngressSpec(ctx, r, tls, clusterLocalServices, tc.Targets)
	if err != nil {
		return nil, err
	}
	return &v1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.Ingress(r),
			Namespace: r.Namespace,
			Labels: map[string]string{
				serving.RouteLabelKey:          r.Name,
				serving.RouteNamespaceLabelKey: r.Namespace,
			},
			Annotations: resources.UnionMaps(map[string]string{
				networking.IngressClassAnnotationKey: ingressClass,
			}, r.ObjectMeta.Annotations),
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

		rules = append(rules, *makeIngressRule(
			routeDomains, r.Namespace, isClusterLocal, targets[name]))
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

func makeIngressRule(domains []string, ns string, isClusterLocal bool, targets traffic.RevisionTargets) *v1alpha1.IngressRule {
	// Optimistically allocate |targets| elements.
	splits := make([]v1alpha1.IngressBackendSplit, 0, len(targets))
	for _, t := range targets {
		if t.Percent == 0 {
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
			Percent: t.Percent,
			// TODO(nghia): Append headers per-split.
			// AppendHeaders: map[string]string{
			// 	activator.RevisionHeaderName:      t.TrafficTarget.RevisionName,
			// 	activator.RevisionHeaderNamespace: ns,
			// },
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
				AppendHeaders: map[string]string{
					activator.RevisionHeaderName:      maxInactive(targets),
					activator.RevisionHeaderNamespace: ns,
				},
			}},
		},
	}
}

// maxInactive constructs Splits for the inactive targets, and add into given IngressPath.
func maxInactive(targets traffic.RevisionTargets) string {
	revisionName, inactiveRevisionName := "", ""
	maxTargetPercent := 0         // There must be a non-zero target if things add to 100
	maxInactiveTargetPercent := 0 // There must be a non-zero target if things add to 100

	for _, t := range targets {
		if t.Percent == 0 {
			continue
		}
		if t.Percent >= maxTargetPercent {
			revisionName = t.RevisionName
			maxTargetPercent = t.Percent
		}
		if t.Active {
			continue
		}
		if t.Percent >= maxInactiveTargetPercent {
			inactiveRevisionName = t.RevisionName
			maxInactiveTargetPercent = t.Percent
		}
	}
	if inactiveRevisionName != "" {
		return inactiveRevisionName
	}
	return revisionName
}

// GetIngressTypeName returns ingress type name: ClusterIngress or Ingress
func GetIngressTypeName(ingress netv1alpha1.IngressAccessor) string {
	if ingress.GetNamespace() == "" {
		return "ClusterIngress"
	} else {
		return "Ingress"
	}
}
