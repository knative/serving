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
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	netapi "knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/kmeta"
	pkgnet "knative.dev/pkg/network"
	apiConfig "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/traffic"

	. "knative.dev/serving/pkg/testing/v1"
)

var (
	r = Route("test-ns", "test-route")

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

	expectedPorts = []corev1.ServicePort{{
		Name:       netapi.ServicePortNameH2C,
		Port:       int32(80),
		TargetPort: intstr.FromInt(80),
	}}
)

func TestMakeK8SService(t *testing.T) {
	tests := []struct {
		name    string
		route   *v1.Route
		status  netv1alpha1.IngressStatus
		tagName string
		private bool

		expectedMeta        metav1.ObjectMeta
		expectedServiceSpec corev1.ServiceSpec
		expectedSubsets     []corev1.EndpointSubset
		expectErr           bool
	}{{
		name:      "no loadbalancer",
		route:     r,
		status:    netv1alpha1.IngressStatus{},
		expectErr: true,
	}, {
		name:  "empty loadbalancer status",
		route: r,
		status: netv1alpha1.IngressStatus{
			PublicLoadBalancer: &netv1alpha1.LoadBalancerStatus{},
		},
		expectErr: true,
	}, {
		name:  "multiple-ingresses",
		route: r,
		status: netv1alpha1.IngressStatus{
			PublicLoadBalancer: &netv1alpha1.LoadBalancerStatus{
				Ingress: []netv1alpha1.LoadBalancerIngressStatus{{Domain: "x"}, {Domain: "y"}},
			},
		},
		expectErr: true,
	}, {
		name:  "misconfigured ingresses",
		route: r,
		status: netv1alpha1.IngressStatus{
			PublicLoadBalancer: &netv1alpha1.LoadBalancerStatus{
				Ingress: []netv1alpha1.LoadBalancerIngressStatus{{}},
			},
		},
		expectErr: true,
	}, {
		name:  "IP",
		route: r,
		status: netv1alpha1.IngressStatus{
			PublicLoadBalancer: &netv1alpha1.LoadBalancerStatus{
				Ingress: []netv1alpha1.LoadBalancerIngressStatus{{
					IP: "some-ip",
				}},
			},
		},
		expectedMeta: expectedMeta,
		expectedServiceSpec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Type:      corev1.ServiceTypeClusterIP,
			Ports:     expectedPorts,
		},
		expectedSubsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{
				IP: "some-ip",
			}},
			Ports: []corev1.EndpointPort{{
				Name: netapi.ServicePortNameH2C,
				Port: int32(80),
			}},
		}},
	}, {
		name:  "Domain",
		route: r,
		status: netv1alpha1.IngressStatus{
			PublicLoadBalancer: &netv1alpha1.LoadBalancerStatus{
				Ingress: []netv1alpha1.LoadBalancerIngressStatus{{
					Domain: "some-domain",
				}},
			},
		},
		expectedMeta: expectedMeta,
		expectedServiceSpec: corev1.ServiceSpec{
			ExternalName: "some-domain",
			Type:         corev1.ServiceTypeExternalName,
			Ports:        expectedPorts,
		},
	}, {
		name:  "DomainInternal",
		route: r,
		status: netv1alpha1.IngressStatus{
			PublicLoadBalancer: &netv1alpha1.LoadBalancerStatus{
				Ingress: []netv1alpha1.LoadBalancerIngressStatus{{
					DomainInternal: "some-internal-domain",
				}},
			},
		},
		expectedMeta: expectedMeta,
		expectedServiceSpec: corev1.ServiceSpec{
			ExternalName:    "some-internal-domain",
			Type:            corev1.ServiceTypeExternalName,
			SessionAffinity: corev1.ServiceAffinityNone,
			Ports:           expectedPorts,
		},
	}, {
		name:  "Mesh",
		route: r,
		status: netv1alpha1.IngressStatus{
			PublicLoadBalancer: &netv1alpha1.LoadBalancerStatus{
				Ingress: []netv1alpha1.LoadBalancerIngressStatus{{
					MeshOnly: true,
				}},
			},
		},
		expectedMeta: expectedMeta,
		expectedServiceSpec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{
				Name: netapi.ServicePortNameHTTP1,
				Port: netapi.ServiceHTTPPort,
			}},
		},
	}, {
		name:  "prioritize IP over DomainInternal",
		route: r,
		status: netv1alpha1.IngressStatus{
			PublicLoadBalancer: &netv1alpha1.LoadBalancerStatus{
				Ingress: []netv1alpha1.LoadBalancerIngressStatus{{
					IP:             "some-ip",
					DomainInternal: "some-internal-domain",
				}},
			},
		},
		expectedMeta: expectedMeta,
		expectedServiceSpec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Type:      corev1.ServiceTypeClusterIP,
			Ports:     expectedPorts,
		},
		expectedSubsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{
				IP: "some-ip",
			}},
			Ports: []corev1.EndpointPort{{
				Name: netapi.ServicePortNameH2C,
				Port: int32(80),
			}},
		}},
	}, {
		name:  "prioritize DomainInternal over Domain",
		route: r,
		status: netv1alpha1.IngressStatus{
			PublicLoadBalancer: &netv1alpha1.LoadBalancerStatus{
				Ingress: []netv1alpha1.LoadBalancerIngressStatus{{
					DomainInternal: "some-internal-domain",
					Domain:         "some-domain",
				}},
			},
		},
		expectedMeta: expectedMeta,
		expectedServiceSpec: corev1.ServiceSpec{
			Type:            corev1.ServiceTypeExternalName,
			ExternalName:    "some-internal-domain",
			SessionAffinity: corev1.ServiceAffinityNone,
			Ports:           expectedPorts,
		},
	}, {
		name:  "prioritize private loadbalancer over public one",
		route: r,
		status: netv1alpha1.IngressStatus{
			PublicLoadBalancer: &netv1alpha1.LoadBalancerStatus{
				Ingress: []netv1alpha1.LoadBalancerIngressStatus{{
					IP: "some-ip",
				}},
			},
			PrivateLoadBalancer: &netv1alpha1.LoadBalancerStatus{
				Ingress: []netv1alpha1.LoadBalancerIngressStatus{{
					Domain: "some-domain",
				}},
			},
		},
		expectedMeta: expectedMeta,
		expectedServiceSpec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: "some-domain",
			Ports:        expectedPorts,
		},
	}, {
		name:    "error when the user wants a private lb but don't have one",
		route:   r,
		private: true,
		status: netv1alpha1.IngressStatus{
			PublicLoadBalancer: &netv1alpha1.LoadBalancerStatus{
				Ingress: []netv1alpha1.LoadBalancerIngressStatus{{
					IP: "some-ip",
				}},
			},
		},
		expectErr: true,
	}, {
		name:    "when the service has a tag name",
		tagName: "some-tag",
		route:   r,
		status: netv1alpha1.IngressStatus{
			PublicLoadBalancer: &netv1alpha1.LoadBalancerStatus{
				Ingress: []netv1alpha1.LoadBalancerIngressStatus{{
					Domain: "some-domain",
				}},
			},
		},
		expectedMeta: metav1.ObjectMeta{
			Name:      "some-tag-test-route",
			Namespace: r.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*kmeta.NewControllerRef(r),
			},
			Labels: map[string]string{
				serving.RouteLabelKey: r.Name,
			},
		},
		expectedServiceSpec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: "some-domain",
			Ports:        expectedPorts,
		},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ingress := &netv1alpha1.Ingress{Status: tc.status}
			ctx := config.ToContext(context.Background(), testConfig())

			got, err := MakeK8sService(ctx, tc.route, tc.tagName, ingress, tc.private)
			if tc.expectErr && err == nil {
				t.Fatal("MakeK8sService returned success but expected error")
			} else if tc.expectErr && err != nil {
				return
			} else if err != nil {
				t.Fatal("MakeK8sService unexpected error:", err)
			}

			if diff := cmp.Diff(tc.expectedMeta, got.Service.ObjectMeta); diff != "" {
				t.Fatal("MakeK8sService service meta diff (-want, +got):\n", diff)
			}
			if diff := cmp.Diff(tc.expectedServiceSpec, got.Service.Spec); diff != "" {
				t.Fatal("MakeK8sService service spec diff (-want, +got):\n", diff)
			}

			if len(tc.expectedSubsets) != 0 {
				if diff := cmp.Diff(tc.expectedMeta, got.Endpoints.ObjectMeta); diff != "" {
					t.Fatal("MakeK8sService endpoints meta diff (-want, +got):\n", diff)
				}
				if diff := cmp.Diff(got.Endpoints.ObjectMeta, got.Service.ObjectMeta); diff != "" {
					t.Fatal("MakeK8sService endpoints.meta != service.meta diff (-want, +got):\n", diff)
				}
				if diff := cmp.Diff(tc.expectedSubsets, got.Endpoints.Subsets); diff != "" {
					t.Fatal("MakeK8sService subsets diff (-want, +got):\n", diff)
				}
			} else if got.Endpoints != nil {
				t.Fatalf("MakeK8sService returned unexpected endpoints")
			}
		})
	}
}

func TestMakePlaceholderService(t *testing.T) {
	tests := []struct {
		name                 string
		route                *v1.Route
		expectedLabels       map[string]string
		expectedAnnos        map[string]string
		expectedExternalName string
	}{{
		name:                 "default public domain route",
		route:                Route("test-ns", "test-route"),
		expectedExternalName: "foo-test-route.test-ns.example.com",
		expectedLabels: map[string]string{
			serving.RouteLabelKey: "test-route",
		},
	}, {
		name: "labels and annotations are propagated from the route",
		route: Route("test-ns", "test-route",
			WithRouteLabel(map[string]string{"route-label": "foo"}),
			WithRouteAnnotation(map[string]string{"route-anno": "bar"}),
		),
		expectedExternalName: "foo-test-route.test-ns.example.com",
		expectedLabels: map[string]string{
			serving.RouteLabelKey: "test-route",
			"route-label":         "foo",
		},
		expectedAnnos: map[string]string{
			"route-anno": "bar",
		},
	}, {
		name: "cluster local route",
		route: Route("test-ns", "test-route",
			WithRouteLabel(map[string]string{netapi.VisibilityLabelKey: serving.VisibilityClusterLocal})),
		expectedExternalName: pkgnet.GetServiceHostname("foo-test-route", "test-ns"),
		expectedLabels: map[string]string{
			serving.RouteLabelKey: "test-route",
		},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			ctx := config.ToContext(context.Background(), cfg)

			target := traffic.RevisionTarget{
				TrafficTarget: v1.TrafficTarget{
					Tag: "foo",
				},
			}

			got, err := MakeK8sPlaceholderService(ctx, tt.route, target.Tag)
			if err != nil {
				t.Errorf("MakeK8sPlaceholderService() error = %v", err)
				return
			}

			if got == nil {
				t.Fatal("Unexpected nil service")
			}

			if diff := cmp.Diff(tt.expectedLabels, got.GetLabels()); diff != "" {
				t.Error("Unexpected Labels (-want +got):", diff)
			}
			if diff := cmp.Diff(tt.expectedAnnos, got.GetAnnotations()); diff != "" {
				t.Error("Unexpected Annotations (-want +got):", diff)
			}

			expectedServiceSpec := corev1.ServiceSpec{
				Type:            corev1.ServiceTypeExternalName,
				ExternalName:    tt.expectedExternalName,
				SessionAffinity: corev1.ServiceAffinityNone,
				Ports: []corev1.ServicePort{{
					Name:       netapi.ServicePortNameH2C,
					Port:       int32(80),
					TargetPort: intstr.FromInt(80),
				}},
			}

			if diff := cmp.Diff(expectedServiceSpec, got.Spec); diff != "" {
				t.Error("Unexpected ServiceSpec (-want +got):", diff)
			}
		})
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
		Network: &netcfg.Config{
			DefaultIngressClass: "test-ingress-class",
			DomainTemplate:      netcfg.DefaultDomainTemplate,
			TagTemplate:         netcfg.DefaultTagTemplate,
			SystemInternalTLS:   netcfg.EncryptionDisabled,
		},
		Features: &apiConfig.Features{
			MultiContainer:               apiConfig.Disabled,
			PodSpecAffinity:              apiConfig.Disabled,
			PodSpecFieldRef:              apiConfig.Disabled,
			PodSpecDryRun:                apiConfig.Enabled,
			PodSpecHostAliases:           apiConfig.Disabled,
			PodSpecNodeSelector:          apiConfig.Disabled,
			PodSpecTolerations:           apiConfig.Disabled,
			PodSpecVolumesEmptyDir:       apiConfig.Disabled,
			PodSpecPersistentVolumeClaim: apiConfig.Disabled,
			PodSpecPersistentVolumeWrite: apiConfig.Disabled,
			PodSpecInitContainers:        apiConfig.Disabled,
			PodSpecPriorityClassName:     apiConfig.Disabled,
			PodSpecSchedulerName:         apiConfig.Disabled,
			TagHeaderBasedRouting:        apiConfig.Disabled,
		},
	}
}
