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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	istiov1alpha1 "github.com/knative/pkg/apis/istio/common/v1alpha1"
	"github.com/knative/pkg/apis/istio/v1alpha3"
	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/traffic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMakeVirtualServiceSpec_CorrectMetadata(t *testing.T) {
	targets := map[string][]traffic.RevisionTarget{}
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
			Labels:    map[string]string{"route": "test-route"},
		},
		Status: v1alpha1.RouteStatus{Domain: "domain.com"},
	}
	expected := metav1.ObjectMeta{
		Name:      "test-route",
		Namespace: "test-ns",
		Labels:    map[string]string{"route": "test-route"},
		OwnerReferences: []metav1.OwnerReference{
			*kmeta.NewControllerRef(r),
		},
	}
	meta := MakeVirtualService(r, &traffic.TrafficConfig{Targets: targets}).ObjectMeta
	if diff := cmp.Diff(expected, meta); diff != "" {
		t.Errorf("Unexpected metadata (-want +got): %v", diff)
	}
}

func TestMakeVirtualServiceSpec_CorrectSpec(t *testing.T) {
	targets := map[string][]traffic.RevisionTarget{}
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
			Labels:    map[string]string{"route": "test-route"},
		},
		Status: v1alpha1.RouteStatus{Domain: "domain.com"},
	}
	expected := v1alpha3.VirtualServiceSpec{
		// We want to connect to two Gateways: the Route's ingress Gateway, and the 'mesh' Gateway.  The former
		// provides access from outside of the cluster, and the latter provides access for services from inside
		// the cluster.
		Gateways: []string{
			"knative-shared-gateway.knative-serving.svc.cluster.local",
			"mesh",
		},
		Hosts: []string{
			"*.domain.com",
			"domain.com",
			"test-route.test-ns.svc.cluster.local",
		},
	}
	routes := MakeVirtualService(r, &traffic.TrafficConfig{Targets: targets}).Spec
	if diff := cmp.Diff(expected, routes); diff != "" {
		t.Errorf("Unexpected routes (-want +got): %v", diff)
	}
}

func TestMakeVirtualServiceSpec_CorrectRoutes(t *testing.T) {
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
			Labels:    map[string]string{"route": "test-route"},
		},
		Status: v1alpha1.RouteStatus{Domain: "domain.com"},
	}
	expected := []v1alpha3.HTTPRoute{{
		Match: []v1alpha3.HTTPMatchRequest{{
			Authority: &istiov1alpha1.StringMatch{Exact: "domain.com"},
		}, {
			Authority: &istiov1alpha1.StringMatch{Exact: "test-route.test-ns.svc.cluster.local"},
		}, {
			Authority: &istiov1alpha1.StringMatch{Exact: "test-route.test-ns.svc"},
		}, {
			Authority: &istiov1alpha1.StringMatch{Exact: "test-route.test-ns"},
		}, {
			Authority: &istiov1alpha1.StringMatch{Exact: "test-route"},
		}},
		Route: []v1alpha3.DestinationWeight{{
			Destination: v1alpha3.Destination{
				Host: "v2-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Timeout: DefaultRouteTimeout,
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      DefaultRouteRetryAttempts,
			PerTryTimeout: DefaultRouteTimeout,
		},
	}, {
		Match: []v1alpha3.HTTPMatchRequest{{
			Authority: &istiov1alpha1.StringMatch{Exact: "v1.domain.com"},
		}},
		Route: []v1alpha3.DestinationWeight{{
			Destination: v1alpha3.Destination{
				Host: "v1-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Timeout: DefaultRouteTimeout,
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      DefaultRouteRetryAttempts,
			PerTryTimeout: DefaultRouteTimeout,
		},
	}}
	routes := MakeVirtualService(r, &traffic.TrafficConfig{Targets: targets}).Spec.Http
	if diff := cmp.Diff(expected, routes); diff != "" {
		fmt.Printf("%+v\n", routes)
		fmt.Printf("%+v\n", expected)
		t.Errorf("Unexpected routes (-want +got): %v", diff)
	}
}

func TestGetRouteDomains_NamelessTarget(t *testing.T) {
	r := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "test-ns",
			Labels: map[string]string{
				"route": "test-route",
			},
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
			Labels: map[string]string{
				"route": "test-route",
			},
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
func TestMakeVirtualServiceRoute_Vanilla(t *testing.T) {
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
	route := makeVirtualServiceRoute(domains, ns, targets)
	expected := v1alpha3.HTTPRoute{
		Match: []v1alpha3.HTTPMatchRequest{{
			Authority: &istiov1alpha1.StringMatch{Exact: "a.com"},
		}, {
			Authority: &istiov1alpha1.StringMatch{Exact: "b.org"},
		}},
		Route: []v1alpha3.DestinationWeight{{
			Destination: v1alpha3.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Timeout: DefaultRouteTimeout,
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      DefaultRouteRetryAttempts,
			PerTryTimeout: DefaultRouteTimeout,
		},
	}
	if diff := cmp.Diff(&expected, route); diff != "" {
		t.Errorf("Unexpected route  (-want +got): %v", diff)
	}
}

func TestMakeVirtualServiceRoute_ZeroPercentTarget(t *testing.T) {
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
	route := makeVirtualServiceRoute(domains, ns, targets)
	expected := v1alpha3.HTTPRoute{
		Match: []v1alpha3.HTTPMatchRequest{{
			Authority: &istiov1alpha1.StringMatch{Exact: "test.org"},
		}},
		Route: []v1alpha3.DestinationWeight{{
			Destination: v1alpha3.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Timeout: DefaultRouteTimeout,
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      DefaultRouteRetryAttempts,
			PerTryTimeout: DefaultRouteTimeout,
		},
	}
	if diff := cmp.Diff(&expected, route); diff != "" {
		t.Errorf("Unexpected route  (-want +got): %v", diff)
	}
}

// Two active targets.
func TestMakeVirtualServiceRoute_TwoTargets(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           90,
		},
		Active: true,
	}, {
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           10,
		},
		Active: true,
	}}
	domains := []string{"test.org"}
	ns := "test-ns"
	route := makeVirtualServiceRoute(domains, ns, targets)
	expected := v1alpha3.HTTPRoute{
		Match: []v1alpha3.HTTPMatchRequest{{
			Authority: &istiov1alpha1.StringMatch{Exact: "test.org"},
		}},
		Route: []v1alpha3.DestinationWeight{{
			Destination: v1alpha3.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 90,
		}, {
			Destination: v1alpha3.Destination{
				Host: "new-revision-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 10,
		}},
		Timeout: DefaultRouteTimeout,
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      DefaultRouteRetryAttempts,
			PerTryTimeout: DefaultRouteTimeout,
		},
	}
	if diff := cmp.Diff(&expected, route); diff != "" {
		t.Errorf("Unexpected route  (-want +got): %v", diff)
	}
}

// One Inactive target.
func TestMakeVirtualServiceRoute_VanillaScaledToZero(t *testing.T) {
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
	route := makeVirtualServiceRoute(domains, ns, targets)
	expected := v1alpha3.HTTPRoute{
		Match: []v1alpha3.HTTPMatchRequest{{
			Authority: &istiov1alpha1.StringMatch{Exact: "a.com"},
		}, {
			Authority: &istiov1alpha1.StringMatch{Exact: "b.org"},
		}},
		Route: []v1alpha3.DestinationWeight{{
			Destination: v1alpha3.Destination{
				Host: "activator-service.knative-serving.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		AppendHeaders: map[string]string{
			"knative-serving-revision":  "revision",
			"knative-serving-namespace": "test-ns",
		},
		Timeout: DefaultRouteTimeout,
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      DefaultRouteRetryAttempts,
			PerTryTimeout: DefaultRouteTimeout,
		},
	}
	if diff := cmp.Diff(&expected, route); diff != "" {
		t.Errorf("Unexpected route  (-want +got): %v", diff)
	}
}

// Two inactive targets.
func TestMakeVirtualServiceRoute_TwoInactiveTargets(t *testing.T) {
	targets := []traffic.RevisionTarget{{
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "config",
			RevisionName:      "revision",
			Percent:           90,
		},
		Active: false,
	}, {
		TrafficTarget: v1alpha1.TrafficTarget{
			ConfigurationName: "new-config",
			RevisionName:      "new-revision",
			Percent:           10,
		},
		Active: false,
	}}
	domains := []string{"test.org"}
	ns := "test-ns"
	route := makeVirtualServiceRoute(domains, ns, targets)
	expected := v1alpha3.HTTPRoute{
		Match: []v1alpha3.HTTPMatchRequest{{
			Authority: &istiov1alpha1.StringMatch{Exact: "test.org"},
		}},
		Route: []v1alpha3.DestinationWeight{{
			Destination: v1alpha3.Destination{
				Host: "activator-service.knative-serving.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		AppendHeaders: map[string]string{
			"knative-serving-revision":  "revision",
			"knative-serving-namespace": "test-ns",
		},
		Timeout: DefaultRouteTimeout,
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      DefaultRouteRetryAttempts,
			PerTryTimeout: DefaultRouteTimeout,
		},
	}
	if diff := cmp.Diff(&expected, route); diff != "" {
		t.Errorf("Unexpected route  (-want +got): %v", diff)
	}
}

// Named target scaled to 0.
func TestMakeVirtualServiceRoute_ZeroPercentNamedTargetScaledToZero(t *testing.T) {
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
	route := makeVirtualServiceRoute(domains, ns, targets)
	expected := v1alpha3.HTTPRoute{
		Match: []v1alpha3.HTTPMatchRequest{{
			Authority: &istiov1alpha1.StringMatch{Exact: "test.org"},
		}},
		Route: []v1alpha3.DestinationWeight{{
			Destination: v1alpha3.Destination{
				Host: "revision-service.test-ns.svc.cluster.local",
				Port: v1alpha3.PortSelector{Number: 80},
			},
			Weight: 100,
		}},
		Timeout: DefaultRouteTimeout,
		Retries: &v1alpha3.HTTPRetry{
			Attempts:      DefaultRouteRetryAttempts,
			PerTryTimeout: DefaultRouteTimeout,
		},
	}
	if diff := cmp.Diff(&expected, route); diff != "" {
		t.Errorf("Unexpected route  (-want +got): %v", diff)
	}
}
