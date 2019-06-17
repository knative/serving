/*
Copyright 2018 The Knative Authors.

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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/kmeta"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	"github.com/knative/serving/pkg/gc"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler/route/config"
	"github.com/knative/serving/pkg/reconciler/route/traffic"
)

var (
	r = &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
		},
	}
	expectedMeta = metav1.ObjectMeta{
		Name:      "test-route",
		Namespace: "test-ns",
		OwnerReferences: []metav1.OwnerReference{
			*kmeta.NewControllerRef(r),
		},
		Labels: map[string]string{
			serving.RouteLabelKey: r.Name,
		},
	}
)

func TestNewMakeK8SService(t *testing.T) {
	scenarios := map[string]struct {
		// Inputs
		route        *v1alpha1.Route
		ingress      *netv1alpha1.ClusterIngress
		targetName   string
		expectedSpec corev1.ServiceSpec
		expectedMeta metav1.ObjectMeta
		shouldFail   bool
	}{
		"no-loadbalancer": {
			route: r,
			ingress: &netv1alpha1.ClusterIngress{
				Status: netv1alpha1.IngressStatus{},
			},
			expectedMeta: expectedMeta,
			shouldFail:   true,
		},
		"empty-loadbalancer": {
			route: r,
			ingress: &netv1alpha1.ClusterIngress{
				Status: netv1alpha1.IngressStatus{
					LoadBalancer: &netv1alpha1.LoadBalancerStatus{
						Ingress: []netv1alpha1.LoadBalancerIngressStatus{{}},
					},
				},
			},
			expectedMeta: expectedMeta,
			shouldFail:   true,
		},
		"multi-loadbalancer": {
			route: r,
			ingress: &netv1alpha1.ClusterIngress{
				Status: netv1alpha1.IngressStatus{
					LoadBalancer: &netv1alpha1.LoadBalancerStatus{
						Ingress: []netv1alpha1.LoadBalancerIngressStatus{{
							Domain: "domain.com",
						}, {
							DomainInternal: "domain.com",
						}},
					},
				},
			},
			expectedMeta: expectedMeta,
			shouldFail:   true,
		},
		"ingress-with-domain": {
			route: r,
			ingress: &netv1alpha1.ClusterIngress{
				Status: netv1alpha1.IngressStatus{
					LoadBalancer: &netv1alpha1.LoadBalancerStatus{
						Ingress: []netv1alpha1.LoadBalancerIngressStatus{{Domain: "domain.com"}},
					},
				},
			},
			expectedMeta: expectedMeta,
			expectedSpec: corev1.ServiceSpec{
				Type:         corev1.ServiceTypeExternalName,
				ExternalName: "domain.com",
			},
		},
		"ingress-with-domaininternal": {
			route: r,
			ingress: &netv1alpha1.ClusterIngress{
				Status: netv1alpha1.IngressStatus{
					LoadBalancer: &netv1alpha1.LoadBalancerStatus{
						Ingress: []netv1alpha1.LoadBalancerIngressStatus{{DomainInternal: "istio-ingressgateway.istio-system.svc.cluster.local"}},
					},
				},
			},
			expectedMeta: expectedMeta,
			expectedSpec: corev1.ServiceSpec{
				Type:            corev1.ServiceTypeExternalName,
				ExternalName:    "istio-ingressgateway.istio-system.svc.cluster.local",
				SessionAffinity: corev1.ServiceAffinityNone,
			},
		},
		"ingress-with-only-mesh": {
			route: r,
			ingress: &netv1alpha1.ClusterIngress{
				Status: netv1alpha1.IngressStatus{
					LoadBalancer: &netv1alpha1.LoadBalancerStatus{
						Ingress: []netv1alpha1.LoadBalancerIngressStatus{{MeshOnly: true}},
					},
				},
			},
			expectedMeta: expectedMeta,
			expectedSpec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{{
					Name: "http",
					Port: 80,
				}},
			},
		},
		"with-target-name-specified": {
			route:      r,
			targetName: "my-target-name",
			ingress: &netv1alpha1.ClusterIngress{
				Status: netv1alpha1.IngressStatus{
					LoadBalancer: &netv1alpha1.LoadBalancerStatus{
						Ingress: []netv1alpha1.LoadBalancerIngressStatus{{MeshOnly: true}},
					},
				},
			},
			expectedMeta: metav1.ObjectMeta{
				Name:      "my-target-name-test-route",
				Namespace: r.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					*kmeta.NewControllerRef(r),
				},
				Labels: map[string]string{
					serving.RouteLabelKey: r.Name,
				},
			},
			expectedSpec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{{
					Name: "http",
					Port: 80,
				}},
			},
		},
	}

	for name, scenario := range scenarios {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			ctx := config.ToContext(context.Background(), cfg)
			service, err := MakeK8sService(ctx, scenario.route, scenario.targetName, scenario.ingress)
			// Validate
			if scenario.shouldFail && err == nil {
				t.Errorf("Test %q failed: returned success but expected error", name)
			}
			if !scenario.shouldFail {
				if err != nil {
					t.Errorf("Test %q failed: returned error: %v", name, err)
				}

				if !cmp.Equal(scenario.expectedMeta, service.ObjectMeta) {
					t.Errorf("Unexpected Metadata (-want +got): %s", cmp.Diff(scenario.expectedMeta, service.ObjectMeta))
				}
				if !cmp.Equal(scenario.expectedSpec, service.Spec) {
					t.Errorf("Unexpected ServiceSpec (-want +got): %s", cmp.Diff(scenario.expectedSpec, service.Spec))
				}
			}
		})
	}
}

func TestMakePlaceholderK8sService(t *testing.T) {
	target := traffic.RevisionTarget{
		TrafficTarget: v1beta1.TrafficTarget{
			Tag: "foo",
		},
	}

	cfg := testConfig()
	ctx := config.ToContext(context.Background(), cfg)

	service, err := MakeK8sPlaceholderService(ctx, r, target.Tag)
	expectedMeta := metav1.ObjectMeta{
		Name:      target.Tag + "-" + r.Name,
		Namespace: r.Namespace,
		OwnerReferences: []metav1.OwnerReference{
			*kmeta.NewControllerRef(r),
		},
		Labels: map[string]string{
			serving.RouteLabelKey: r.Name,
		},
	}
	expectedSpec := corev1.ServiceSpec{
		Type:            corev1.ServiceTypeExternalName,
		ExternalName:    "foo-test-route.test-ns.example.com",
		SessionAffinity: corev1.ServiceAffinityNone,
	}

	if err != nil {
		t.Errorf("Unexpected error %v returned", err)
	}

	if !cmp.Equal(expectedMeta, service.ObjectMeta) {
		t.Errorf("Unexpected Metadata (-want +got): %s", cmp.Diff(expectedMeta, service.ObjectMeta))
	}
	if !cmp.Equal(expectedSpec, service.Spec) {
		t.Errorf("Unexpected ServiceSpec (-want +got): %s", cmp.Diff(expectedSpec, service.Spec))
	}
}

func TestSelectorFromRoute(t *testing.T) {
	selector := SelectorFromRoute(r)
	if !selector.Matches(labels.Set{serving.RouteLabelKey: r.Name}) {
		t.Errorf("Unexpected labels in selector")
	}
}

func testConfig() *config.Config {
	return &config.Config{
		Domain: &config.Domain{
			Domains: map[string]*config.LabelSelector{
				"example.com": {},
				"another-example.com": {
					Selector: map[string]string{"app": "prod"},
				},
			},
		},
		Network: &network.Config{
			DefaultClusterIngressClass: "test-ingress-class",
			DomainTemplate:             network.DefaultDomainTemplate,
			TagTemplate:                network.DefaultTagTemplate,
		},
		GC: &gc.Config{
			StaleRevisionLastpinnedDebounce: time.Duration(1 * time.Minute),
		},
	}
}
