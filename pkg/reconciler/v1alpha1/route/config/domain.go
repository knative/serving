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

package config

import (
	"fmt"
	"strings"

	"github.com/ghodss/yaml"
	corev1 "k8s.io/api/core/v1"
)

const (
	DomainConfigName = "config-domain"

	// ClusterLocalDomain is the domain suffix used for cluster local
	// routes. Currently we do not yet have a way to figure out this
	// information programmatically.
	//
	// TODO(nghia): Extract this to a common network reconciler
	// setting, or find an K8s API to discover this information.
	ClusterLocalDomain = "svc.cluster.local"

	// VisibilityLabelKey is the label to indicate visibility of Route
	// and KServices.  It can be an annotation too but since users are
	// already using labels for domain, it probably best to keep this
	// consistent.
	VisibilityLabelKey = "serving.knative.dev/visibility"
	// VisibilityClusterLocal is the label value for VisibilityLabelKey
	// that will result to the Route/KService getting a cluster local
	// domain suffix.
	VisibilityClusterLocal = "cluster-local"
)

// LabelSelector represents map of {key,value} pairs. A single {key,value} in the
// map is equivalent to a requirement key == value. The requirements are ANDed.
type LabelSelector struct {
	Selector map[string]string `json:"selector,omitempty"`
}

func (s *LabelSelector) specificity() int {
	return len(s.Selector)
}

// Matches returns whether the given labels meet the requirement of the selector.
func (s *LabelSelector) Matches(labels map[string]string) bool {
	for label, expectedValue := range s.Selector {
		value, ok := labels[label]
		if !ok || expectedValue != value {
			return false
		}
	}
	return true
}

// Domain maps domains to routes by matching the domain's
// label selectors to the route's labels.
type Domain struct {
	// Domains map from domain to label selector.  If a route has
	// labels matching a particular selector, it will use the
	// corresponding domain.  If multiple selectors match, we choose
	// the most specific selector.
	Domains map[string]*LabelSelector
}

// NewDomainFromConfigMap creates a Domain from the supplied ConfigMap
func NewDomainFromConfigMap(configMap *corev1.ConfigMap) (*Domain, error) {
	c := Domain{Domains: map[string]*LabelSelector{}}
	hasDefault := false
	for k, v := range configMap.Data {
		labelSelector := LabelSelector{}
		err := yaml.Unmarshal([]byte(v), &labelSelector)
		if err != nil {
			return nil, err
		}
		c.Domains[k] = &labelSelector
		if len(labelSelector.Selector) == 0 {
			hasDefault = true
		}
	}
	if !hasDefault {
		return nil, fmt.Errorf("Config %#v must have a default domain", configMap.Data)
	}
	return &c, nil
}

// LookupDomainForLabels returns a domain given a set of labels.
// Since we reject configuration without a default domain, this should
// always return a value.
func (c *Domain) LookupDomainForLabels(labels map[string]string) string {
	domain := ""
	specificity := -1
	// If we see VisibilityLabelKey sets with VisibilityClusterLocal, that
	// will take precedence and the route will get a ClusterLocalDomain.
	if l, _ := labels[VisibilityLabelKey]; l == VisibilityClusterLocal {
		return ClusterLocalDomain
	}
	for k, selector := range c.Domains {
		// Ignore if selector doesn't match, or decrease the specificity.
		if !selector.Matches(labels) || selector.specificity() < specificity {
			continue
		}
		if selector.specificity() > specificity || strings.Compare(k, domain) < 0 {
			domain = k
			specificity = selector.specificity()
		}
	}

	return domain
}
