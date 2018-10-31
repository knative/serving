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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/kmeta"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/traffic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestMakeClusterIngress_CorrectMetadata(t *testing.T) {
	targets := map[string][]traffic.RevisionTarget{}
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
		Status: v1alpha1.RouteStatus{Domain: "domain.com"},
	}
	expected := metav1.ObjectMeta{
		GenerateName: "test-route-",
		Labels: map[string]string{
			serving.RouteLabelKey:          "test-route",
			serving.RouteNamespaceLabelKey: "test-ns",
		},
		OwnerReferences: []metav1.OwnerReference{
			*kmeta.NewControllerRef(r),
		},
	}
	meta := MakeClusterIngress(r, &traffic.TrafficConfig{Targets: targets}).ObjectMeta
	if diff := cmp.Diff(expected, meta); diff != "" {
		t.Errorf("Unexpected metadata (-want +got): %v", diff)
	}
}

func TestMakeClusterIngressSpec_CorrectRules(t *testing.T) {
	targets := map[string][]traffic.RevisionTarget{
		"": {{
			TrafficTarget: v1alpha1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v2",
				Percent:           100,
			},
			Active: true,
		}},
		"v1": {{
			TrafficTarget: v1alpha1.TrafficTarget{
				ConfigurationName: "config",
				RevisionName:      "v1",
				Percent:           100,
			},
			Active: true,
		}},
	}
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
		Status: v1alpha1.RouteStatus{Domain: "domain.com"},
	}
	expected := []netv1alpha1.ClusterIngressRule{{
		Hosts: []string{
			"domain.com",
			"test-route.test-ns.svc.cluster.local",
			"test-route.test-ns.svc",
			"test-route.test-ns",
			"test-route",
		},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "v2-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
				Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
				Retries: &netv1alpha1.HTTPRetry{
					PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Attempts:      netv1alpha1.DefaultRetryCount,
				},
			}},
		},
	}, {
		Hosts: []string{"v1.domain.com"},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "v1-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
				Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
				Retries: &netv1alpha1.HTTPRetry{
					PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Attempts:      netv1alpha1.DefaultRetryCount,
				},
			}},
		},
	}}
	rules := makeClusterIngressSpec(r, targets).Rules
	if diff := cmp.Diff(expected, rules); diff != "" {
		fmt.Printf("%+v\n", rules)
		fmt.Printf("%+v\n", expected)
		t.Errorf("Unexpected rules (-want +got): %v", diff)
	}
}

func TestGetRouteDomains_NamelessTarget(t *testing.T) {
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
	}
	base := "domain.com"
	expected := []string{base,
		"test-route.test-ns.svc.cluster.local",
		"test-route.test-ns.svc",
		"test-route.test-ns",
		"test-route",
	}
	domains := getRouteDomains("", r, base)
	if diff := cmp.Diff(expected, domains); diff != "" {
		t.Errorf("Unexpected domains  (-want +got): %v", diff)
	}
}

func TestGetRouteDomains_NamedTarget(t *testing.T) {
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
	}
	name := "v1"
	base := "domain.com"
	expected := []string{"v1.domain.com"}
	domains := getRouteDomains(name, r, base)
	if diff := cmp.Diff(expected, domains); diff != "" {
		t.Errorf("Unexpected domains  (-want +got): %v", diff)
	}
}

// One active target.
func TestMakeClusterIngressRule_Vanilla(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           100,
		},
		Active: true,
	}}
	domains := []string{"a.com", "b.org"}
	ns := "test-ns"
	rule := makeClusterIngressRule(domains, ns, targets)
	expected := netv1alpha1.ClusterIngressRule{
		Hosts: []string{
			"a.com",
			"b.org",
		},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "revision-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
				Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
				Retries: &netv1alpha1.HTTPRetry{
					PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Attempts:      netv1alpha1.DefaultRetryCount,
				},
			}},
		},
	}

	if diff := cmp.Diff(&expected, rule); diff != "" {
		t.Errorf("Unexpected rule (-want +got): %v", diff)
	}
}

// One active target and a target of zero percent.
func TestMakeClusterIngressRule_ZeroPercentTarget(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           100,
		},
		Active: true,
	}, {
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           0,
		},
		Active: true,
	}}
	domains := []string{"test.org"}
	ns := "test-ns"
	rule := makeClusterIngressRule(domains, ns, targets)
	expected := netv1alpha1.ClusterIngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "revision-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
				Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
				Retries: &netv1alpha1.HTTPRetry{
					PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Attempts:      netv1alpha1.DefaultRetryCount,
				},
			}},
		},
	}

	if diff := cmp.Diff(&expected, rule); diff != "" {
		t.Errorf("Unexpected rule (-want +got): %v", diff)
	}
}

// Two active targets.
func TestMakeClusterIngressRule_TwoTargets(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           80,
		},
		Active: true,
	}, {
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           20,
		},
		Active: true,
	}}
	domains := []string{"test.org"}
	ns := "test-ns"
	rule := makeClusterIngressRule(domains, ns, targets)
	expected := netv1alpha1.ClusterIngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "revision-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 80,
				}, {
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "new-revision-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 20,
				}},
				Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
				Retries: &netv1alpha1.HTTPRetry{
					PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Attempts:      netv1alpha1.DefaultRetryCount,
				},
			}},
		},
	}

	if diff := cmp.Diff(&expected, rule); diff != "" {
		t.Errorf("Unexpected rule (-want +got): %v", diff)
	}
}

// Inactive target.
func TestMakeClusterIngressRule_InactiveTarget(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           100,
		},
		Active: false,
	}}
	domains := []string{"a.com", "b.org"}
	ns := "test-ns"
	rule := makeClusterIngressRule(domains, ns, targets)
	expected := netv1alpha1.ClusterIngressRule{
		Hosts: []string{
			"a.com",
			"b.org",
		},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "knative-serving",
						ServiceName:      "activator-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
				AppendHeaders: map[string]string{
					"knative-serving-revision":  "revision",
					"knative-serving-namespace": "test-ns",
				},
				Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
				Retries: &netv1alpha1.HTTPRetry{
					PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Attempts:      netv1alpha1.DefaultRetryCount,
				},
			}},
		},
	}
	if diff := cmp.Diff(&expected, rule); diff != "" {
		t.Errorf("Unexpected rule (-want +got): %v", diff)
	}
}

// Two inactive targets.
func TestMakeClusterIngressRule_TwoInactiveTargets(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           80,
		},
		Active: false,
	}, {
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           20,
		},
		Active: false,
	}}
	domains := []string{"a.com", "b.org"}
	ns := "test-ns"
	rule := makeClusterIngressRule(domains, ns, targets)
	expected := netv1alpha1.ClusterIngressRule{
		Hosts: []string{
			"a.com",
			"b.org",
		},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "knative-serving",
						ServiceName:      "activator-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
				AppendHeaders: map[string]string{
					"knative-serving-revision":  "revision",
					"knative-serving-namespace": "test-ns",
				},
				Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
				Retries: &netv1alpha1.HTTPRetry{
					PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Attempts:      netv1alpha1.DefaultRetryCount,
				},
			}},
		},
	}
	if diff := cmp.Diff(&expected, rule); diff != "" {
		t.Errorf("Unexpected rule (-want +got): %v", diff)
	}
}

func TestMakeClusterIngressRule_ZeroPercentTargetInactive(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           100,
		},
		Active: true,
	}, {
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           0,
		},
		Active: false,
	}}
	domains := []string{"test.org"}
	ns := "test-ns"
	rule := makeClusterIngressRule(domains, ns, targets)
	expected := netv1alpha1.ClusterIngressRule{
		Hosts: []string{"test.org"},
		HTTP: &netv1alpha1.HTTPClusterIngressRuleValue{
			Paths: []netv1alpha1.HTTPClusterIngressPath{{
				Splits: []netv1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: netv1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "revision-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
				Timeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
				Retries: &netv1alpha1.HTTPRetry{
					PerTryTimeout: &metav1.Duration{Duration: netv1alpha1.DefaultTimeout},
					Attempts:      netv1alpha1.DefaultRetryCount,
				},
			}},
		},
	}

	if diff := cmp.Diff(&expected, rule); diff != "" {
		t.Errorf("Unexpected rule (-want +got): %v", diff)
	}
}
