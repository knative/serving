// +build e2e

/*
Copyright 2019 The Knative Authors

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
package e2e

import (
	"fmt"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"

	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	routeconfig "knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"

	. "knative.dev/serving/pkg/testing/v1alpha1"
)

// In this test, we set up two apps: helloworld and httpproxy.
// helloworld is a simple app that displays a plaintext string with private visibility.
// httpproxy is a proxy that redirects request to internal service of helloworld app
// with {tag}-{route}.{namespace}.svc.cluster.local, or {tag}-{route}.{namespace}.svc, or {tag}-{route}.{namespace}.
// The expected result is that the request sent to httpproxy app is successfully redirected
// to helloworld app when trying to communicate via local address only.
func TestSubrouteLocalSTS(t *testing.T) { // We can't use a longer more descriptive name because routes will fail DNS checks. (Max 64 characters)
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)

	t.Log("Creating a Service for the helloworld test app.")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	tag := "current"

	withInternalVisibility := WithServiceLabel(routeconfig.VisibilityLabelKey, routeconfig.VisibilityClusterLocal)
	withTrafficSpec := WithInlineRouteSpec(v1alpha1.RouteSpec{
		Traffic: []v1alpha1.TrafficTarget{
			{
				TrafficTarget: v1.TrafficTarget{
					Tag:     tag,
					Percent: ptr.Int64(100),
				},
			},
		},
	})

	resources, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, withInternalVisibility, withTrafficSpec)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	t.Logf("helloworld internal domain is %s.", resources.Route.Status.URL.Host)

	// helloworld app and its route are ready. Running the test cases now.
	for _, tc := range testCases {
		domain := fmt.Sprintf("%s-%s", tag, resources.Route.Status.Address.URL.Host)
		helloworldDomain := strings.TrimSuffix(domain, tc.suffix)
		t.Run(tc.name, func(t *testing.T) {
			testProxyToHelloworld(t, clients, helloworldDomain, true, false)
		})
	}
}

func TestSubrouteVisibilityChange(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)

	t.Log("Creating a Service for the helloworld test app.")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	tag := "my-tag"

	withInternalVisibility := WithServiceLabel(
		routeconfig.VisibilityLabelKey, routeconfig.VisibilityClusterLocal)
	withTrafficSpec := WithInlineRouteSpec(v1alpha1.RouteSpec{
		Traffic: []v1alpha1.TrafficTarget{
			{
				TrafficTarget: v1.TrafficTarget{
					Tag:     tag,
					Percent: ptr.Int64(100),
				},
			},
		},
	})
	resources, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, withInternalVisibility, withTrafficSpec)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// Ensure that it only has cluster local addresses
	for _, traffic := range resources.Route.Status.Traffic {
		if !strings.HasSuffix(traffic.TrafficTarget.URL.Host, network.GetClusterDomainName()) {
			t.Fatalf("Expected all subroutes to be cluster local")
		}
	}

	serviceName := fmt.Sprintf("%s-%s", tag, resources.Route.Name)
	svc, err := clients.KubeClient.Kube.CoreV1().Services(test.ServingNamespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get k8s service to modify: %s", err.Error())
	}

	svcCopy := svc.DeepCopy()
	labels.SetVisibility(&svcCopy.ObjectMeta, false)

	patch, err := duck.CreatePatch(svc, svcCopy)
	if err != nil {
		t.Fatalf("Failed to create patch: %s", err.Error())
	}

	encPatch, err := patch.MarshalJSON()
	if err != nil {
		t.Fatalf("Failed to marshal patch: %s", err.Error())
	}
	_, err = clients.KubeClient.Kube.CoreV1().Services(test.ServingNamespace).Patch(serviceName, types.JSONPatchType, encPatch)
	if err != nil {
		t.Fatalf("Failed to patch service: %s", err.Error())
	}

	// Get updated route to check.
	err = v1a1test.WaitForRouteState(clients.ServingAlphaClient, resources.Route.Name, func(r *v1alpha1.Route) (bool, error) {
		for _, traffic := range r.Status.Traffic {
			if traffic.TrafficTarget.Tag == tag && strings.HasSuffix(traffic.TrafficTarget.URL.Host, network.GetClusterDomainName()) {
				return true, nil
			}
		}
		return false, nil
	}, "TrafficUrlUpdated")

	if err != nil {
		t.Fatalf("Timed out waiting for traffic url to change: %s", err.Error())
	}
}
