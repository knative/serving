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

package config

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	netapi "knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/network"
	"knative.dev/pkg/system"

	. "knative.dev/pkg/configmap/testing"
	_ "knative.dev/pkg/system/testing"
)

func TestSelectorMatches(t *testing.T) {
	selector := LabelSelector{
		Selector: map[string]string{
			"app":     "bar",
			"version": "beta",
		},
	}
	nonMatchingLabels := []map[string]string{
		{"app": "bar"},
		{"version": "beta"},
		{"app": "foo"},
		{},
	}
	matchingLabels := []map[string]string{
		{"app": "bar", "version": "beta"},
		{"app": "bar", "version": "beta", "last_updated": "yesterday"},
		{"app": "bar", "version": "beta", "deployer": "Felicity Smoak"},
	}
	for _, labels := range nonMatchingLabels {
		if selector.Matches(labels) {
			t.Errorf("Expect selector %v not to match labels %v", selector, labels)
		}
	}
	for _, labels := range matchingLabels {
		if !selector.Matches(labels) {
			t.Errorf("Expect selector %v to match labels %v", selector, labels)
		}
	}
}

func TestNewConfigNoEntry(t *testing.T) {
	d, err := NewDomainFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      DomainConfigName,
		},
	})
	if err != nil {
		t.Error("Unexpected error when config file has no entry:", err)
	}
	got := d.LookupDomainForLabels(nil)
	if got != DefaultDomain {
		t.Errorf("LookupDomainForLabels() = %s, wanted %s", got, DefaultDomain)
	}
}

func TestNewConfigBadYaml(t *testing.T) {
	c, err := NewDomainFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      DomainConfigName,
		},
		Data: map[string]string{
			"default.com": "bad: yaml: all: day",
		},
	})
	if err == nil {
		t.Errorf("NewDomainFromConfigMap() = %v, wanted error", c)
	}
}

func TestNewConfig(t *testing.T) {
	expectedConfig := Domain{
		Domains: map[string]DomainConfig{
			"test-domain.foo.com": {
				Selector: &LabelSelector{
					Selector: map[string]string{
						"app": "foo",
					},
				},
			},
			"bar.com": {
				Selector: &LabelSelector{
					Selector: map[string]string{
						"app":     "bar",
						"version": "beta",
					},
				},
				Type: DomainTypeWildcard,
			},
			"default.com": {Selector: &LabelSelector{}, Type: DomainTypeWildcard},
		},
	}
	c, err := NewDomainFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      DomainConfigName,
		},
		Data: map[string]string{
			"test-domain.foo.com": "selector:\n  app: foo\n",
			"bar.com":             "selector:\n  app: bar\n  version: beta\ntype: wildcard",
			"default.com":         "",
		},
	})
	if err != nil {
		t.Error("Unexpected error:", err)
	}
	if diff := cmp.Diff(&expectedConfig, c); diff != "" {
		t.Error("Unexpected config diff (-want +got):", diff)
	}
}

func TestLookupDomainForLabels(t *testing.T) {
	config := Domain{
		Domains: map[string]DomainConfig{
			"test-domain.foo.com": {
				Selector: &LabelSelector{
					Selector: map[string]string{
						"app": "foo",
					},
				},
			},
			"foo.com": {
				Selector: &LabelSelector{
					Selector: map[string]string{
						"app":     "foo",
						"version": "prod",
					},
				},
			},
			"bar.com": {
				Selector: &LabelSelector{
					Selector: map[string]string{
						"app": "bar",
					},
				},
			},
			"default.com": {Selector: &LabelSelector{}, Type: DomainTypeWildcard},
		},
	}

	expectations := []struct {
		labels map[string]string
		domain string
	}{{
		labels: map[string]string{"app": "foo"},
		domain: "test-domain.foo.com",
	}, {
		// This should match two selector, but the one with version=prod is more specific.
		labels: map[string]string{"app": "foo", "version": "prod"},
		domain: "foo.com",
	}, {
		labels: map[string]string{"app": "bar"},
		domain: "bar.com",
	}, {
		labels: map[string]string{"app": "bar", "version": "whatever"},
		domain: "bar.com",
	}, {
		labels: map[string]string{"app": "whatever"},
		domain: "default.com",
	}, {
		labels: map[string]string{},
		domain: "default.com",
	}, {
		labels: map[string]string{netapi.VisibilityLabelKey: "cluster-local"},
		domain: "svc." + network.GetClusterDomainName(),
	}}

	for _, expected := range expectations {
		domain := config.LookupDomainForLabels(expected.labels)
		if expected.domain != domain {
			t.Errorf("Expected domain %q got %q", expected.domain, domain)
		}
	}
}

func TestOurDomain(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, DomainConfigName)
	if _, err := NewDomainFromConfigMap(cm); err != nil {
		t.Error("NewDomainFromConfigMap(actual) =", err)
	}
	if _, err := NewDomainFromConfigMap(example); err != nil {
		t.Error("NewDomainFromConfigMap(example) =", err)
	}
}
