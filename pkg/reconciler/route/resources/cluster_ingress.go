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
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	servingv1alpha1 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler/route/domains"
	"github.com/knative/serving/pkg/reconciler/route/resources/names"
	"github.com/knative/serving/pkg/reconciler/route/traffic"
	"github.com/knative/serving/pkg/resources"
)

// IsClusterLocal checks if a Route is publicly visible or only visible with cluster.
func IsClusterLocal(r *servingv1alpha1.Route) bool {
	return r.Status.URL != nil && strings.HasSuffix(r.Status.URL.Host, network.GetClusterDomainName())
}

// MakeClusterIngressTLS creates ClusterIngressTLS to configure the ingress TLS.
func MakeClusterIngressTLS(cert *v1alpha1.Certificate, hostNames []string) v1alpha1.ClusterIngressTLS {
	return v1alpha1.ClusterIngressTLS{
		Hosts:           hostNames,
		SecretName:      cert.Spec.SecretName,
		SecretNamespace: cert.Namespace,
	}
}

// MakeClusterIngress creates ClusterIngress to set up routing rules. Such ClusterIngress specifies
// which Hosts that it applies to, as well as the routing rules.
func MakeClusterIngress(ctx context.Context, r *servingv1alpha1.Route, tc *traffic.Config, tls []v1alpha1.ClusterIngressTLS, ingressClass string) (*v1alpha1.ClusterIngress, error) {
	spec, err := makeClusterIngressSpec(ctx, r, tls, tc.Targets)
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

func makeClusterIngressSpec(ctx context.Context, r *servingv1alpha1.Route, tls []v1alpha1.ClusterIngressTLS, targets map[string]traffic.RevisionTargets) (v1alpha1.IngressSpec, error) {
	// Domain should have been specified in route status
	// before calling this func.
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	// Sort the names to give things a deterministic ordering.
	sort.Strings(names)

	// The routes are matching rule based on domain name to traffic split targets.
	rules := make([]v1alpha1.ClusterIngressRule, 0, len(names))
	for _, name := range names {
		domains, err := routeDomains(ctx, name, r)
		if err != nil {
			return v1alpha1.IngressSpec{}, err
		}
		rules = append(rules, *makeClusterIngressRule(
			domains, r.Namespace, targets[name]))
	}

	visibility := v1alpha1.IngressVisibilityExternalIP
	if IsClusterLocal(r) {
		visibility = v1alpha1.IngressVisibilityClusterLocal
	}

	return v1alpha1.IngressSpec{
		Rules:      rules,
		Visibility: visibility,
		TLS:        tls,
	}, nil
}

func routeDomains(ctx context.Context, targetName string, r *servingv1alpha1.Route) ([]string, error) {
	fullName, err := domains.DomainNameFromTemplate(ctx, r, domains.SubdomainName(r, targetName))
	if err != nil {
		return nil, err
	}

	ruleDomains := []string{fullName}

	// TODO(andrew-su): We are adding this for backwards compatibility. This should be removed when
	// we feel the users had sufficient time to move away from the deprecated name.
	if r.Status.URL != nil {
		deprecatedFullName := traffic.DeprecatedTagDomain(targetName, r.Status.URL.Host)
		if fullName != deprecatedFullName {
			ruleDomains = append(ruleDomains, deprecatedFullName)
		}
	}

	if targetName == traffic.DefaultTarget {
		// The default target is also referred to by its internal K8s
		// generated domain name.
		internalHost := names.K8sServiceFullname(r)
		if internalHost != "" && ruleDomains[0] != internalHost {
			ruleDomains = append(ruleDomains, internalHost)
		}
	}

	return ruleDomains, nil
}

func makeClusterIngressRule(domains []string, ns string, targets traffic.RevisionTargets) *v1alpha1.ClusterIngressRule {
	// Optimistically allocate |targets| elements.
	splits := make([]v1alpha1.ClusterIngressBackendSplit, 0, len(targets))
	for _, t := range targets {
		if t.Percent == 0 {
			continue
		}

		splits = append(splits, v1alpha1.ClusterIngressBackendSplit{
			ClusterIngressBackend: v1alpha1.ClusterIngressBackend{
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

	return &v1alpha1.ClusterIngressRule{
		Hosts: domains,
		HTTP: &v1alpha1.HTTPClusterIngressRuleValue{
			Paths: []v1alpha1.HTTPClusterIngressPath{{
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
