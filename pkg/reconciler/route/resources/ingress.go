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
	"context"
	"encoding/json"
	"sort"

	"github.com/davecgh/go-spew/spew"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"

	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	ingress "knative.dev/networking/pkg/ingress"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/activator"
	apicfg "knative.dev/serving/pkg/apis/config"
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
	return MakeIngressWithRollout(
		// If no rollout is specified, we just build the default one.
		ctx, r, tc, tc.BuildRollout(), tls, ingressClass, acmeChallenges...)
}

// MakeIngressWithRollout builds a KIngress object from the given parameters.
// When building the ingress the builder will take into the account
// the desired rollout state to split the traffic.
func MakeIngressWithRollout(
	ctx context.Context,
	r *servingv1.Route,
	tc *traffic.Config,
	ro *traffic.Rollout,
	tls []netv1alpha1.IngressTLS,
	ingressClass string,
	acmeChallenges ...netv1alpha1.HTTP01Challenge,
) (*netv1alpha1.Ingress, error) {
	spec, err := makeIngressSpec(ctx, r, tls, tc, ro, acmeChallenges...)
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
				networking.RolloutAnnotationKey:      serializeRollout(ctx, ro),
			}, r.GetAnnotations()), func(key string) bool {
				return key == corev1.LastAppliedConfigAnnotation
			}),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(r)},
		},
		Spec: spec,
	}, nil
}

func serializeRollout(ctx context.Context, r *traffic.Rollout) string {
	sr, err := json.Marshal(r)
	if err != nil {
		// This must never happen in the normal course of things.
		logging.FromContext(ctx).Warnw("Error serializing Rollout: "+spew.Sprint(r),
			zap.Error(err))
		return ""
	}
	return string(sr)
}

// makeIngressSpec builds a new IngressSpec from inputs.
func makeIngressSpec(
	ctx context.Context,
	r *servingv1.Route,
	tls []netv1alpha1.IngressTLS,
	tc *traffic.Config,
	ro *traffic.Rollout,
	acmeChallenges ...netv1alpha1.HTTP01Challenge,
) (netv1alpha1.IngressSpec, error) {
	// Domain should have been specified in route status
	// before calling this func.
	names := make([]string, 0, len(tc.Targets))
	for name := range tc.Targets {
		names = append(names, name)
	}
	// Sort the names to give things a deterministic ordering.
	sort.Strings(names)
	// The routes are matching rule based on domain name to traffic split targets.
	rules := make([]netv1alpha1.IngressRule, 0, len(names))

	featuresConfig := config.FromContextOrDefaults(ctx).Features

	for _, name := range names {
		visibilities := []netv1alpha1.IngressVisibility{netv1alpha1.IngressVisibilityClusterLocal}
		// If this is a public target (or not being marked as cluster-local), we also make public rule.
		if v, ok := tc.Visibility[name]; !ok || v == netv1alpha1.IngressVisibilityExternalIP {
			visibilities = append(visibilities, netv1alpha1.IngressVisibilityExternalIP)
		}
		for _, visibility := range visibilities {
			domains, err := routeDomain(ctx, name, r, visibility)
			if err != nil {
				return netv1alpha1.IngressSpec{}, err
			}
			rule := makeIngressRule(domains, r.Namespace,
				visibility, tc.Targets[name], ro.RolloutsByTag(name))
			if featuresConfig.TagHeaderBasedRouting == apicfg.Enabled {
				if rule.HTTP.Paths[0].AppendHeaders == nil {
					rule.HTTP.Paths[0].AppendHeaders = make(map[string]string, 1)
				}

				if name == traffic.DefaultTarget {
					// To provide information if a request is routed via the "default route" or not,
					// the header "Knative-Serving-Default-Route: true" is appended here.
					// If the header has "true" and there is a "Knative-Serving-Tag" header,
					// then the request is having the undefined tag header,
					// which will be observed in queue-proxy.
					rule.HTTP.Paths[0].AppendHeaders[network.DefaultRouteHeaderName] = "true"

					// Add ingress paths for a request with the tag header.
					// If a request has one of the `names` (tag name), specified as the
					// Knative-Serving-Tag header, except for the DefaultTarget,
					// the request will be routed to that target.
					// corresponding to the tag name.
					// Since names are sorted `DefaultTarget == ""` is the first one,
					// so just pass the subslice.
					rule.HTTP.Paths = append(
						makeTagBasedRoutingIngressPaths(r.Namespace, tc, ro, names[1:]), rule.HTTP.Paths...)
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
					MakeACMEIngressPaths(acmeChallenges, domains...), rule.HTTP.Paths...)
			}
			rules = append(rules, rule)
		}
	}

	var httpOption netv1alpha1.HTTPOption

	switch config.FromContext(ctx).Network.HTTPProtocol {
	case network.HTTPEnabled:
		httpOption = netv1alpha1.HTTPOptionEnabled
	case network.HTTPRedirected:
		httpOption = netv1alpha1.HTTPOptionRedirected
	// This will be deprecated soon
	case network.HTTPDisabled:
		httpOption = ""
	}

	return netv1alpha1.IngressSpec{
		Rules:      rules,
		TLS:        tls,
		HTTPOption: httpOption,
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
		return nil, err
	}

	meta := r.ObjectMeta.DeepCopy()
	isClusterLocal := visibility == netv1alpha1.IngressVisibilityClusterLocal
	labels.SetVisibility(meta, isClusterLocal)

	domain, err := domains.DomainNameFromTemplate(ctx, *meta, hostname)
	if err != nil {
		return nil, err
	}
	domains := []string{domain}
	if isClusterLocal {
		domains = ingress.ExpandedHosts(sets.NewString(domains...)).List()
	}
	return domains, err
}

// MakeACMEIngressPaths returns a set of netv1alpha1.HTTPIngressPath
// that can be used to perform ACME challenges.
func MakeACMEIngressPaths(acmeChallenges []netv1alpha1.HTTP01Challenge, domains ...string) []netv1alpha1.HTTPIngressPath {
	challenges := getChallengeHosts(acmeChallenges)
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

func makeIngressRule(domains []string, ns string,
	visibility netv1alpha1.IngressVisibility,
	targets traffic.RevisionTargets,
	roCfgs []*traffic.ConfigurationRollout) netv1alpha1.IngressRule {
	return netv1alpha1.IngressRule{
		Hosts:      domains,
		Visibility: visibility,
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{
				*makeBaseIngressPath(ns, targets, roCfgs),
			},
		},
	}
}

// `names` must not include `""` — the DefaultTarget.
func makeTagBasedRoutingIngressPaths(ns string, tc *traffic.Config, ro *traffic.Rollout, names []string) []netv1alpha1.HTTPIngressPath {
	paths := make([]netv1alpha1.HTTPIngressPath, 0, len(names))

	for _, name := range names {
		path := makeBaseIngressPath(ns, tc.Targets[name], ro.RolloutsByTag(name))
		path.Headers = map[string]netv1alpha1.HeaderMatch{network.TagHeaderName: {Exact: name}}
		paths = append(paths, *path)
	}

	return paths
}

func rolloutConfig(cfgName string, ros []*traffic.ConfigurationRollout) *traffic.ConfigurationRollout {
	idx := sort.Search(len(ros), func(i int) bool {
		return ros[i].ConfigurationName >= cfgName
	})
	// Technically impossible with valid inputs.
	if idx >= len(ros) {
		return nil
	}
	return ros[idx]
}

func makeBaseIngressPath(ns string, targets traffic.RevisionTargets,
	roCfgs []*traffic.ConfigurationRollout) *netv1alpha1.HTTPIngressPath {
	// Optimistically allocate |targets| elements.
	splits := make([]netv1alpha1.IngressBackendSplit, 0, len(targets))
	for _, t := range targets {
		var cfg *traffic.ConfigurationRollout
		if *t.Percent == 0 {
			continue
		}

		if t.LatestRevision != nil && *t.LatestRevision {
			cfg = rolloutConfig(t.ConfigurationName, roCfgs)
		}
		if cfg == nil || len(cfg.Revisions) < 2 {
			// No rollout in progress.
			splits = append(splits, netv1alpha1.IngressBackendSplit{
				IngressBackend: netv1alpha1.IngressBackend{
					ServiceNamespace: ns,
					ServiceName:      t.RevisionName,
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
		} else {
			for i := range cfg.Revisions {
				rev := &cfg.Revisions[i]
				splits = append(splits, netv1alpha1.IngressBackendSplit{
					IngressBackend: netv1alpha1.IngressBackend{
						ServiceNamespace: ns,
						ServiceName:      rev.RevisionName,
						// Port on the public service must match port on the activator.
						// Otherwise, the serverless services can't guarantee seamless positive handoff.
						ServicePort: intstr.FromInt(networking.ServicePort(t.Protocol)),
					},
					Percent: rev.Percent,
					AppendHeaders: map[string]string{
						activator.RevisionHeaderName:      rev.RevisionName,
						activator.RevisionHeaderNamespace: ns,
					},
				})
			}
		}
	}

	return &netv1alpha1.HTTPIngressPath{
		Splits: splits,
	}
}
