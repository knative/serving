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
	"errors"
	"sort"

	"github.com/davecgh/go-spew/spew"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/serving/pkg/reconciler/route/domains"

	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	netcfg "knative.dev/networking/pkg/config"
	netheader "knative.dev/networking/pkg/http/header"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/activator"
	apicfg "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	servingnetworking "knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/reconciler/route/config"
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

// MakeIngress creates per-tag Ingress resources to set up routing rules.
// Each returned Ingress specifies the Hosts it applies to and the routing
// rules for a single traffic tag.
func MakeIngress(
	ctx context.Context,
	r *servingv1.Route,
	tc *traffic.Config,
	tls []netv1alpha1.IngressTLS,
	ingressClass string,
	acmeChallenges ...netv1alpha1.HTTP01Challenge,
) ([]*netv1alpha1.Ingress, error) {
	return MakeIngressWithRollout(
		// If no rollout is specified, we just build the default one.
		ctx, r, tc, tc.BuildRollout(), tls, ingressClass, acmeChallenges...)
}

// MakeIngressWithRollout builds per-tag KIngress objects from the given parameters.
// It creates an ingress for each traffic target (including the default target),
// using the provided rollout to determine traffic splitting.
// Internally delegates to buildTagIngress for each tag.
func MakeIngressWithRollout(
	ctx context.Context,
	r *servingv1.Route,
	tc *traffic.Config,
	ro *traffic.Rollout,
	tls []netv1alpha1.IngressTLS,
	ingressClass string,
	acmeChallenges ...netv1alpha1.HTTP01Challenge,
) ([]*netv1alpha1.Ingress, error) {
	// Get sorted tag names for deterministic ordering.
	tagNames := make([]string, 0, len(tc.Targets))
	for name := range tc.Targets {
		tagNames = append(tagNames, name)
	}
	sort.Strings(tagNames)

	// If there are no targets, still create a default ingress with empty rules
	// to maintain backward compatibility.
	if len(tagNames) == 0 {
		tagNames = []string{traffic.DefaultTarget}
	}

	featuresConfig := config.FromContextOrDefaults(ctx).Features
	networkConfig := config.FromContextOrDefaults(ctx).Network

	httpOption, err := servingnetworking.GetHTTPOption(ctx, config.FromContext(ctx).Network, r.GetAnnotations())
	if err != nil {
		return nil, err
	}

	acmePathsByHost := buildACMEPathsByHost(acmeChallenges)

	ingresses := make([]*netv1alpha1.Ingress, 0, len(tagNames))
	for _, tagName := range tagNames {
		ing, err := buildTagIngress(ctx, r, tc, ro, tagName, tls, ingressClass,
			featuresConfig, networkConfig, httpOption, acmePathsByHost)
		if err != nil {
			return nil, err
		}
		ingresses = append(ingresses, ing)
	}

	return ingresses, nil
}

// MakeDefaultIngressWithRollout creates the default Ingress for the Route,
// incorporating the specified rollout configuration for gradual traffic shifting.
// Rollout is only meaningful for the default ingress.
//
// When TagHeaderBasedRouting is enabled, the provided Rollout must contain
// configurations for all traffic targets (not just the default), because
// the default ingress includes tag-header-based routing paths for other tags.
func MakeDefaultIngressWithRollout(
	ctx context.Context,
	r *servingv1.Route,
	tc *traffic.Config,
	ro *traffic.Rollout,
	tls []netv1alpha1.IngressTLS,
	ingressClass string,
	acmeChallenges ...netv1alpha1.HTTP01Challenge,
) (*netv1alpha1.Ingress, error) {
	featuresConfig := config.FromContextOrDefaults(ctx).Features
	networkConfig := config.FromContextOrDefaults(ctx).Network

	httpOption, err := servingnetworking.GetHTTPOption(ctx, config.FromContext(ctx).Network, r.GetAnnotations())
	if err != nil {
		return nil, err
	}

	acmePathsByHost := buildACMEPathsByHost(acmeChallenges)

	return buildTagIngress(ctx, r, tc, ro, traffic.DefaultTarget, tls, ingressClass,
		featuresConfig, networkConfig, httpOption, acmePathsByHost)
}

// MakeRouteTagIngress creates an Ingress for a specific traffic tag.
// Tag ingresses handle routing for named traffic targets (e.g., "canary", "latest").
// Rollout configuration is derived internally via tc.BuildRollout();
// gradual rollout (multi-step traffic shifting) is primarily a concern of the
// default ingress. For orchestrated creation with an explicit effective rollout,
// use MakeIngressWithRollout instead.
func MakeRouteTagIngress(
	ctx context.Context,
	r *servingv1.Route,
	tc *traffic.Config,
	tagName string,
	tls []netv1alpha1.IngressTLS,
	ingressClass string,
	acmeChallenges ...netv1alpha1.HTTP01Challenge,
) (*netv1alpha1.Ingress, error) {
	if tagName == traffic.DefaultTarget {
		return nil, errors.New("cannot create per-tag ingress for default target: use MakeDefaultIngressWithRollout instead")
	}

	featuresConfig := config.FromContextOrDefaults(ctx).Features
	networkConfig := config.FromContextOrDefaults(ctx).Network

	httpOption, err := servingnetworking.GetHTTPOption(ctx, config.FromContext(ctx).Network, r.GetAnnotations())
	if err != nil {
		return nil, err
	}

	ro := tc.BuildRollout()
	acmePathsByHost := buildACMEPathsByHost(acmeChallenges)

	return buildTagIngress(ctx, r, tc, ro, tagName, tls, ingressClass,
		featuresConfig, networkConfig, httpOption, acmePathsByHost)
}

// buildTagIngress builds a single Ingress for the given traffic tag.
func buildTagIngress(
	ctx context.Context,
	r *servingv1.Route,
	tc *traffic.Config,
	ro *traffic.Rollout,
	tagName string,
	tls []netv1alpha1.IngressTLS,
	ingressClass string,
	featuresConfig *apicfg.Features,
	networkConfig *netcfg.Config,
	httpOption netv1alpha1.HTTPOption,
	acmePathsByHost map[string][]netv1alpha1.HTTPIngressPath,
) (*netv1alpha1.Ingress, error) {
	spec, err := makeIngressSpecForTag(ctx, r, tls, tc, ro, tagName, featuresConfig, networkConfig, httpOption, acmePathsByHost)
	if err != nil {
		return nil, err
	}

	tagRollout := rolloutForTag(ro, tagName)

	labels := map[string]string{
		serving.RouteLabelKey:          r.Name,
		serving.RouteNamespaceLabelKey: r.Namespace,
	}
	if tagName != traffic.DefaultTarget {
		labels[networking.TagLabelKey] = tagName
	}

	return &netv1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.TaggedIngress(r, tagName),
			Namespace: r.Namespace,
			Labels:    kmeta.UnionMaps(r.Labels, labels),
			Annotations: kmeta.FilterMap(kmeta.UnionMaps(map[string]string{
				networking.IngressClassAnnotationKey: ingressClass,
				networking.RolloutAnnotationKey:      serializeRollout(ctx, tagRollout),
			}, r.GetAnnotations()), ExcludedAnnotations.Has),
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(r)},
		},
		Spec: spec,
	}, nil
}

// buildACMEPathsByHost creates a lookup map from host to ACME challenge paths.
func buildACMEPathsByHost(acmeChallenges []netv1alpha1.HTTP01Challenge) map[string][]netv1alpha1.HTTPIngressPath {
	acmePathsByHost := make(map[string][]netv1alpha1.HTTPIngressPath)
	for _, challenge := range acmeChallenges {
		host := challenge.URL.Host
		acmePath := MakeACMEIngressPath(challenge)
		acmePathsByHost[host] = append(acmePathsByHost[host], acmePath)
	}
	return acmePathsByHost
}

// rolloutForTag returns a Rollout containing only the configurations for the given tag.
func rolloutForTag(ro *traffic.Rollout, tagName string) *traffic.Rollout {
	tagRollout := &traffic.Rollout{}
	for _, cfg := range ro.Configurations {
		if cfg.Tag == tagName {
			tagRollout.Configurations = append(tagRollout.Configurations, cfg)
		}
	}
	return tagRollout
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

// makeIngressSpecForTag builds a new IngressSpec for a single traffic tag.
func makeIngressSpecForTag(
	ctx context.Context,
	r *servingv1.Route,
	tls []netv1alpha1.IngressTLS,
	tc *traffic.Config,
	ro *traffic.Rollout,
	tagName string,
	featuresConfig *apicfg.Features,
	networkConfig *netcfg.Config,
	httpOption netv1alpha1.HTTPOption,
	acmePathsByHost map[string][]netv1alpha1.HTTPIngressPath,
) (netv1alpha1.IngressSpec, error) {
	visibilities := []netv1alpha1.IngressVisibility{netv1alpha1.IngressVisibilityClusterLocal}
	// If this is a public target (or not being marked as cluster-local), we also make public rule.
	if v, ok := tc.Visibility[tagName]; !ok || v == netv1alpha1.IngressVisibilityExternalIP {
		visibilities = append(visibilities, netv1alpha1.IngressVisibilityExternalIP)
	}

	var rules []netv1alpha1.IngressRule
	for _, visibility := range visibilities {
		tagDomains, err := domains.GetDomainsForVisibility(ctx, tagName, r, visibility)
		if err != nil {
			return netv1alpha1.IngressSpec{}, err
		}
		domainRules := makeIngressRules(tagDomains, r.Namespace,
			visibility, tc.Targets[tagName], ro.RolloutsByTag(tagName), networkConfig.SystemInternalTLSEnabled())

		// Apply tag header routing and ACME merging to each rule
		for i := range domainRules {
			rule := &domainRules[i]

			if featuresConfig.TagHeaderBasedRouting == apicfg.Enabled {
				if rule.HTTP.Paths[0].AppendHeaders == nil {
					rule.HTTP.Paths[0].AppendHeaders = make(map[string]string, 1)
				}

				if tagName == traffic.DefaultTarget {
					rule.HTTP.Paths[0].AppendHeaders[netheader.DefaultRouteKey] = "true"

					// Add ingress paths for requests with the tag header.
					// Collect non-default tag names (sorted).
					var otherTags []string
					for name := range tc.Targets {
						if name != traffic.DefaultTarget {
							otherTags = append(otherTags, name)
						}
					}
					sort.Strings(otherTags)
					if len(otherTags) > 0 {
						tagPaths := makeTagBasedRoutingIngressPaths(
							r.Namespace, tc, ro, networkConfig.SystemInternalTLSEnabled(), otherTags)
						rule.HTTP.Paths = append(tagPaths, rule.HTTP.Paths...)
					}
				} else {
					rule.HTTP.Paths[0].AppendHeaders[netheader.RouteTagKey] = tagName
				}
			}

			// Merge ACME paths for ExternalIP single-host rules
			// Note: ACME challenges without matching traffic rules are silently ignored
			if visibility == netv1alpha1.IngressVisibilityExternalIP && len(rule.Hosts) == 1 {
				if acmePaths, exists := acmePathsByHost[rule.Hosts[0]]; exists {
					rule.HTTP.Paths = append(acmePaths, rule.HTTP.Paths...)
				}
			}
		}

		rules = append(rules, domainRules...)
	}

	// Filter TLS entries to only include those relevant to this tag's domains.
	tagTLS := filterTLSForRules(tls, rules)

	return netv1alpha1.IngressSpec{
		Rules:      rules,
		TLS:        tagTLS,
		HTTPOption: httpOption,
	}, nil
}

// filterTLSForRules returns the subset of TLS entries whose hosts match
// hosts found in the given ingress rules.
func filterTLSForRules(tls []netv1alpha1.IngressTLS, rules []netv1alpha1.IngressRule) []netv1alpha1.IngressTLS {
	// Collect all hosts from rules
	ruleHosts := sets.New[string]()
	for _, rule := range rules {
		ruleHosts.Insert(rule.Hosts...)
	}

	var filtered []netv1alpha1.IngressTLS
	for _, t := range tls {
		var matchingHosts []string
		for _, h := range t.Hosts {
			if ruleHosts.Has(h) {
				matchingHosts = append(matchingHosts, h)
			}
		}
		if len(matchingHosts) > 0 {
			filtered = append(filtered, netv1alpha1.IngressTLS{
				Hosts:           matchingHosts,
				SecretName:      t.SecretName,
				SecretNamespace: t.SecretNamespace,
			})
		}
	}
	return filtered
}

// MakeACMEIngressPath converts an ACME challenge into an HTTPIngressPath.
func MakeACMEIngressPath(challenge netv1alpha1.HTTP01Challenge) netv1alpha1.HTTPIngressPath {
	return netv1alpha1.HTTPIngressPath{
		Path: challenge.URL.Path,
		Splits: []netv1alpha1.IngressBackendSplit{{
			IngressBackend: netv1alpha1.IngressBackend{
				ServiceNamespace: challenge.ServiceNamespace,
				ServiceName:      challenge.ServiceName,
				ServicePort:      challenge.ServicePort,
			},
			Percent: 100,
		}},
	}
}

func makeIngressRules(domains sets.Set[string], ns string,
	visibility netv1alpha1.IngressVisibility,
	targets traffic.RevisionTargets,
	roCfgs []*traffic.ConfigurationRollout,
	encryption bool,
) []netv1alpha1.IngressRule {
	basePath := makeBaseIngressPath(ns, targets, roCfgs, encryption)

	// ClusterLocal: keep multi-host (no ACME challenges needed)
	if visibility == netv1alpha1.IngressVisibilityClusterLocal {
		return []netv1alpha1.IngressRule{makeIngressRuleForHosts(sets.List(domains), visibility, basePath)}
	}

	// ExternalIP: create one rule per domain (enables per-host ACME challenge merging)
	domainList := sets.List(domains)
	rules := make([]netv1alpha1.IngressRule, 0, len(domainList))
	for _, domain := range domainList {
		rules = append(rules, makeIngressRuleForHosts([]string{domain}, visibility, basePath))
	}
	return rules
}

func makeIngressRuleForHosts(hosts []string, visibility netv1alpha1.IngressVisibility, basePath *netv1alpha1.HTTPIngressPath) netv1alpha1.IngressRule {
	return netv1alpha1.IngressRule{
		Hosts:      hosts,
		Visibility: visibility,
		HTTP: &netv1alpha1.HTTPIngressRuleValue{
			Paths: []netv1alpha1.HTTPIngressPath{*basePath},
		},
	}
}

func makeTagBasedRoutingIngressPaths(ns string, tc *traffic.Config, ro *traffic.Rollout, encryption bool, names []string) []netv1alpha1.HTTPIngressPath {
	paths := make([]netv1alpha1.HTTPIngressPath, 0, len(names))

	for _, name := range names {
		path := makeBaseIngressPath(ns, tc.Targets[name], ro.RolloutsByTag(name), encryption)
		path.Headers = map[string]netv1alpha1.HeaderMatch{netheader.RouteTagKey: {Exact: name}}
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
	roCfgs []*traffic.ConfigurationRollout, encryption bool,
) *netv1alpha1.HTTPIngressPath {
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
		var servicePort intstr.IntOrString
		if encryption {
			servicePort = intstr.FromInt(networking.ServiceHTTPSPort)
		} else {
			servicePort = intstr.FromInt(networking.ServicePort(t.Protocol))
		}
		if cfg == nil || len(cfg.Revisions) < 2 {
			// No rollout in progress.
			splits = append(splits, netv1alpha1.IngressBackendSplit{
				IngressBackend: netv1alpha1.IngressBackend{
					ServiceNamespace: ns,
					ServiceName:      t.RevisionName,
					// Port on the public service must match port on the activator.
					// Otherwise, the serverless services can't guarantee seamless positive handoff.
					ServicePort: servicePort,
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
						ServicePort: servicePort,
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
