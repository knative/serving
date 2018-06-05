/*
Copyright 2018 Google LLC

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

package controller

import (
	"fmt"
	"strings"

	"github.com/ghodss/yaml"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// LabelSelector represents map of {key,value} pairs. A single {key,value} in the
// map is equivalent to a requirement key == value. The requirements are ANDed.
type LabelSelector struct {
	Selector map[string]string `json:"selector,omitempty"`
}

func (s *LabelSelector) specificity() int {
	return len(s.Selector)
}

// Matches() returns whether the given labels meet the requirement of the selector.
func (s *LabelSelector) Matches(labels map[string]string) bool {
	for label, expectedValue := range s.Selector {
		value, ok := labels[label]
		if !ok || expectedValue != value {
			return false
		}
	}
	return true
}

// Config contains controller configurations.
type Config struct {
	// Domains map from domain to label selector.  If a route has
	// labels matching a particular selector, it will use the
	// corresponding domain.  If multiple selectors match, we choose
	// the most specific selector.
	Domains map[string]*LabelSelector
}

const (
	elaNamespace = "knative-serving-system"
)

func NewConfig(kubeClient kubernetes.Interface) (*Config, error) {
	m, err := kubeClient.CoreV1().ConfigMaps(elaNamespace).Get(GetDomainConfigMapName(), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	c := Config{Domains: map[string]*LabelSelector{}}
	hasDefault := false
	for k, v := range m.Data {
		// TODO(josephburnett): migrate domain configuration to k8sflag
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
		return nil, fmt.Errorf("Config %#v must have a default domain", m.Data)
	}
	return &c, nil
}

// LookupDomainForLabels() returns a domain given a set of labels.
// Since we reject configuration without a default domain, this should
// always return a value.
func (c *Config) LookupDomainForLabels(labels map[string]string) string {
	domain := ""
	specificity := -1

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
