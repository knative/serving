/*
Copyright 2018 Google LLC.

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

package route

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg"
	"github.com/knative/serving/pkg/controller"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
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

func TestNewConfigMissingConfigMap(t *testing.T) {
	_, err := NewDomainConfig(fakekubeclientset.NewSimpleClientset())
	if err == nil {
		t.Error("Expect an error when ConfigMap not exists")
	}
}

func TestNewConfigNoEntry(t *testing.T) {
	kubeClient := fakekubeclientset.NewSimpleClientset()
	kubeClient.CoreV1().ConfigMaps(pkg.GetServingSystemNamespace()).Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      controller.GetDomainConfigMapName(),
		},
	})
	_, err := NewDomainConfig(kubeClient)
	if err == nil {
		t.Error("Expect an error when config file has no entry")
	}
}

func TestNewConfig(t *testing.T) {
	expectedConfig := DomainConfig{
		Domains: map[string]*LabelSelector{
			"test-domain.foo.com": {
				Selector: map[string]string{
					"app": "foo",
				},
			},
			"bar.com": {
				Selector: map[string]string{
					"app":     "bar",
					"version": "beta",
				},
			},
			"default.com": {},
		},
	}
	kubeClient := fakekubeclientset.NewSimpleClientset()
	kubeClient.CoreV1().ConfigMaps(pkg.GetServingSystemNamespace()).Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      controller.GetDomainConfigMapName(),
		},
		Data: map[string]string{
			"test-domain.foo.com": "selector:\n  app: foo",
			"bar.com":             "selector:\n  app: bar\n  version: beta",
			"default.com":         "",
		},
	})
	c, err := NewDomainConfig(kubeClient)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if diff := cmp.Diff(&expectedConfig, c); diff != "" {
		t.Errorf("Unexpected config diff (-want +got): %s", diff)
	}
}

func TestLookupDomainForLabels(t *testing.T) {
	config := DomainConfig{
		Domains: map[string]*LabelSelector{
			"test-domain.foo.com": {
				Selector: map[string]string{
					"app": "foo",
				},
			},
			"foo.com": {
				Selector: map[string]string{
					"app":     "foo",
					"version": "prod",
				},
			},
			"bar.com": {
				Selector: map[string]string{
					"app": "bar",
				},
			},
			"default.com": {},
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
	}}

	for _, expected := range expectations {
		domain := config.LookupDomainForLabels(expected.labels)
		if expected.domain != domain {
			t.Errorf("Expected domain %q got %q", expected.domain, domain)
		}
	}
}
