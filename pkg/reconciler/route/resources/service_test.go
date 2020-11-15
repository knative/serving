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
	"time"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"

	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	apiConfig "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/gc"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/traffic"

	. "knative.dev/serving/pkg/testing/v1"
)

var (
	r = Route("test-ns", "test-route")

	ingressSvc = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
		},
		Spec: corev1.ServiceSpec{
			Type: "ClusterIP",
			Ports: []corev1.ServicePort{{
				Name:       "test",
				Port:       int32(80),
				TargetPort: intstr.FromInt(8080),
			}},
		},
	}

	subsets = []corev1.EndpointSubset{{
		Addresses: []corev1.EndpointAddress{{
			IP: "128.0.0.1",
		}},
		Ports: []corev1.EndpointPort{{
			Name: networking.ServicePortNameHTTP1,
			Port: 1234,
		}},
	}}
)

func TestMakeEndpoints(t *testing.T) {
	tests := []struct {
		name            string
		route           *v1.Route
		svc             *corev1.Service
		srcEps          *corev1.Endpoints
		expectedSubsets []corev1.EndpointSubset
		expectedLabels  map[string]string
		expectedAnnos   map[string]string
	}{{
		name: "labels and annotations are propagated from the route",
		route: Route("test-ns", "test-route",
			WithRouteLabel(map[string]string{"route-label": "foo"}), WithRouteAnnotation(map[string]string{"route-anno": "bar"})),
		svc:             ingressSvc,
		srcEps:          &corev1.Endpoints{Subsets: subsets},
		expectedSubsets: subsets,
		expectedLabels: map[string]string{
			serving.RouteLabelKey: "test-route",
			"route-label":         "foo",
		},
		expectedAnnos: map[string]string{
			"route-anno": "bar",
		},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			ctx := config.ToContext(context.Background(), cfg)

			got := MakeEndpoints(ctx, tt.svc, tt.route, tt.srcEps)

			if got == nil {
				t.Fatal("Unexpected nil service")
			}

			if !cmp.Equal(tt.expectedLabels, got.Labels) {
				t.Error("Unexpected Labels (-want +got):", cmp.Diff(tt.expectedLabels, got.Labels))
			}
			if !cmp.Equal(tt.expectedAnnos, got.ObjectMeta.Annotations) {
				t.Error("Unexpected Annotations (-want +got):", cmp.Diff(tt.expectedAnnos, got.ObjectMeta.Annotations))
			}
			if !cmp.Equal(tt.expectedSubsets, got.Subsets) {
				t.Error("Unexpected endpoints subsets (-want +got):", cmp.Diff(tt.expectedSubsets, got.Subsets))
			}
		})
	}
}

func TestMakeK8sPlaceholderService(t *testing.T) {
	tests := []struct {
		name           string
		expectedSpec   corev1.ServiceSpec
		expectedLabels map[string]string
		expectedAnnos  map[string]string
		wantErr        bool
		route          *v1.Route
	}{{
		name:  "default public domain route",
		route: r,
		expectedSpec: corev1.ServiceSpec{
			Type:            corev1.ServiceTypeClusterIP,
			SessionAffinity: corev1.ServiceAffinityNone,
			Ports:           defaultServicePort,
		},
		expectedLabels: map[string]string{
			serving.RouteLabelKey: "test-route",
		},
		wantErr: false,
	}, {
		name: "labels and annotations are propagated from the route",
		route: Route("test-ns", "test-route",
			WithRouteLabel(map[string]string{"route-label": "foo"}), WithRouteAnnotation(map[string]string{"route-anno": "bar"})),
		expectedSpec: corev1.ServiceSpec{
			Type:            corev1.ServiceTypeClusterIP,
			SessionAffinity: corev1.ServiceAffinityNone,
			Ports:           defaultServicePort,
		},
		expectedLabels: map[string]string{
			serving.RouteLabelKey: "test-route",
			"route-label":         "foo",
		},
		expectedAnnos: map[string]string{
			"route-anno": "bar",
		},
		wantErr: false,
	}, {
		name:  "cluster local route",
		route: Route("test-ns", "test-route", WithRouteLabel(map[string]string{network.VisibilityLabelKey: serving.VisibilityClusterLocal})),
		expectedSpec: corev1.ServiceSpec{
			Type:            corev1.ServiceTypeClusterIP,
			SessionAffinity: corev1.ServiceAffinityNone,
			Ports:           defaultServicePort,
		},
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
			if (err != nil) != tt.wantErr {
				t.Errorf("MakeK8sPlaceholderService() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got == nil {
				t.Fatal("Unexpected nil service")
			}

			if !cmp.Equal(tt.expectedLabels, got.Labels) {
				t.Error("Unexpected Labels (-want +got):", cmp.Diff(tt.expectedLabels, got.Labels))
			}
			if !cmp.Equal(tt.expectedAnnos, got.ObjectMeta.Annotations) {
				t.Error("Unexpected Annotations (-want +got):", cmp.Diff(tt.expectedAnnos, got.ObjectMeta.Annotations))
			}
			if !cmp.Equal(tt.expectedSpec, got.Spec) {
				t.Error("Unexpected ServiceSpec (-want +got):", cmp.Diff(tt.expectedSpec, got.Spec))
			}
		})
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
			DefaultIngressClass: "test-ingress-class",
			DomainTemplate:      network.DefaultDomainTemplate,
			TagTemplate:         network.DefaultTagTemplate,
		},
		GC: &gc.Config{
			StaleRevisionLastpinnedDebounce: 1 * time.Minute,
		},
		Features: &apiConfig.Features{
			MultiContainer:        apiConfig.Disabled,
			PodSpecAffinity:       apiConfig.Disabled,
			PodSpecFieldRef:       apiConfig.Disabled,
			PodSpecDryRun:         apiConfig.Enabled,
			PodSpecNodeSelector:   apiConfig.Disabled,
			PodSpecTolerations:    apiConfig.Disabled,
			ResponsiveRevisionGC:  apiConfig.Disabled,
			TagHeaderBasedRouting: apiConfig.Disabled,
		},
	}
}

func TestGetNames(t *testing.T) {
	tests := []struct {
		name     string
		services []*corev1.Service
		want     sets.String
	}{
		{
			name: "nil services",
			want: sets.String{},
		},
		{
			name: "multiple services",
			services: []*corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "svc2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "svc3"}},
			},
			want: sets.NewString("svc1", "svc2", "svc3"),
		},
		{
			name: "duplicate services",
			services: []*corev1.Service{
				{ObjectMeta: metav1.ObjectMeta{Name: "svc1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "svc1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "svc1"}},
			},
			want: sets.NewString("svc1"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetNames(tt.services); !cmp.Equal(got, tt.want) {
				t.Error("GetNames() (-want, +got) =", cmp.Diff(tt.want, got))
			}
		})
	}
}
