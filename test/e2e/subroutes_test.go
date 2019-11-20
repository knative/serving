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
	"time"

	"github.com/pkg/errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/test/logstream"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	routeconfig "knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"
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

	resources, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with https */
		withInternalVisibility, withTrafficSpec)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	t.Logf("helloworld internal domain is %s.", resources.Route.Status.URL.Host)

	// helloworld app and its route are ready. Running the test cases now.
	for _, tc := range testCases {
		domain := fmt.Sprintf("%s-%s", tag, resources.Route.Status.Address.URL.Host)
		helloworldURL := resources.Route.Status.Address.URL.URL()
		helloworldURL.Host = strings.TrimSuffix(domain, tc.suffix)
		t.Run(tc.name, func(t *testing.T) {
			testProxyToHelloworld(t, clients, helloworldURL, true, false)
		})
	}
}

func TestSubrouteVisibilityPublicToPrivate(t *testing.T) {
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

	subrouteTag1 := "my-tag"
	subrouteTag2 := "my-tag2"

	withTrafficSpec := WithInlineRouteSpec(v1alpha1.RouteSpec{
		Traffic: []v1alpha1.TrafficTarget{{
			TrafficTarget: v1.TrafficTarget{
				Tag:     subrouteTag1,
				Percent: ptr.Int64(100),
			},
		}},
	})
	resources, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with https */
		withTrafficSpec)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %w", names.Service, err)
	}

	if isClusterLocal, err := isTrafficClusterLocal(resources.Route.Status.Traffic, subrouteTag1); err != nil {
		t.Fatalf(err.Error())
	} else if isClusterLocal {
		t.Fatalf("Expected subroutes with tag %s to be not cluster local", subrouteTag1)
	}

	if isRouteClusterLocal(resources.Route.Status) {
		t.Fatalf("Expected route to be not cluster local")
	}

	// Update subroute1 to private.
	serviceName := serviceNameForRoute(subrouteTag1, resources.Route.Name)
	svc, err := clients.KubeClient.Kube.CoreV1().Services(test.ServingNamespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get k8s service to modify: %w", err)
	}

	svcCopy := svc.DeepCopy()
	labels.SetVisibility(&svcCopy.ObjectMeta, true)

	svcpatchBytes, err := test.CreateBytePatch(svc, svcCopy)
	if err != nil {
		t.Fatalf(err.Error())
	}

	if _, err = clients.KubeClient.Kube.CoreV1().Services(test.ServingNamespace).Patch(serviceName, types.JSONPatchType, svcpatchBytes); err != nil {
		t.Fatalf("Failed to patch service: %w", err)
	}

	//Create subroute2 in kservice.
	ksvcCopy := resources.Service.DeepCopy()
	ksvcCopyRouteTraffic := append(ksvcCopy.Spec.Traffic,
		v1alpha1.TrafficTarget{
			TrafficTarget: v1.TrafficTarget{
				Tag:            subrouteTag2,
				LatestRevision: ptr.Bool(true),
			},
		})

	if _, err = v1a1test.UpdateServiceRouteSpec(t, clients, names, v1alpha1.RouteSpec{Traffic: ksvcCopyRouteTraffic}); err != nil {
		t.Fatalf("Failed to patch service: %w", err)
	}

	if err = v1a1test.WaitForRouteState(clients.ServingAlphaClient, resources.Route.Name, func(r *v1alpha1.Route) (bool, error) {
		//Check subroute1 is not cluster-local
		if isClusterLocal, err := isTrafficClusterLocal(r.Status.Traffic, subrouteTag1); err != nil {
			return false, err
		} else if !isClusterLocal {
			return false, nil
		}
		//Check subroute2 is cluster local
		if isClusterLocal, err := isTrafficClusterLocal(r.Status.Traffic, subrouteTag2); err != nil {
			return false, nil
		} else if isClusterLocal {
			return false, nil
		}
		return true, nil
	}, "Subroutes are not in correct state"); err != nil {
		t.Fatalf("Expected subroute1 with tag %s to be not cluster local; subroute2 with tag %s to be cluster local: %w", subrouteTag1, subrouteTag2, err)
	}

	//Update route to private.
	ksvclabelCopy := resources.Service.DeepCopy()
	labels.SetVisibility(&ksvclabelCopy.ObjectMeta, true)
	if _, err = v1a1test.PatchService(t, clients, resources.Service, ksvclabelCopy); err != nil {
		t.Fatalf("Failed to patch service: %s", err.Error())
	}

	if err = v1a1test.WaitForRouteState(clients.ServingAlphaClient, resources.Route.Name, func(r *v1alpha1.Route) (bool, error) {
		return isRouteClusterLocal(r.Status), nil
	}, "Route is cluster local"); err != nil {
		t.Fatalf("Route did not become cluster local: %s", err.Error())
	}

	clusterLocalRoute, err := clients.ServingAlphaClient.Routes.Get(resources.Route.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf(err.Error())
	}

	for _, tag := range []string{subrouteTag1, subrouteTag2} {
		if isClusterLocal, err := isTrafficClusterLocal(clusterLocalRoute.Status.Traffic, tag); err != nil {
			t.Fatalf(err.Error())
		} else if !isClusterLocal {
			t.Fatalf("Expected subroute with tag %s to be cluster local", tag)
		}
	}
}

func TestSubrouteVisibilityPrivateToPublic(t *testing.T) {
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

	subrouteTag1 := "my-tag"
	subrouteTag2 := "my-tag2"

	withInternalVisibility := WithServiceLabel(routeconfig.VisibilityLabelKey, routeconfig.VisibilityClusterLocal)
	withTrafficSpec := WithInlineRouteSpec(v1alpha1.RouteSpec{
		Traffic: []v1alpha1.TrafficTarget{
			{
				TrafficTarget: v1.TrafficTarget{
					Tag:     subrouteTag1,
					Percent: ptr.Int64(50),
				},
			},
			{
				TrafficTarget: v1.TrafficTarget{
					Tag:     subrouteTag2,
					Percent: ptr.Int64(50),
				},
			},
		},
	})
	resources, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with https */
		withTrafficSpec, withInternalVisibility)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	for _, tag := range []string{subrouteTag1, subrouteTag2} {
		if isClusterLocal, err := isTrafficClusterLocal(resources.Route.Status.Traffic, tag); err != nil {
			t.Fatalf(err.Error())
		} else if !isClusterLocal {
			t.Fatalf("Expected subroute with tag %s to be cluster local", tag)
		}
	}

	if !isRouteClusterLocal(resources.Route.Status) {
		t.Fatalf("Expected route to be cluster local")
	}

	//Update subroute1 to private
	serviceName := serviceNameForRoute(subrouteTag1, resources.Route.Name)
	svc, err := clients.KubeClient.Kube.CoreV1().Services(test.ServingNamespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get k8s service to modify: %s", err.Error())
	}

	svcCopy := svc.DeepCopy()
	labels.SetVisibility(&svcCopy.ObjectMeta, true)

	svcpatchBytes, err := test.CreateBytePatch(svc, svcCopy)
	if err != nil {
		t.Fatalf(err.Error())
	}

	if _, err = clients.KubeClient.Kube.CoreV1().Services(test.ServingNamespace).Patch(serviceName, types.JSONPatchType, svcpatchBytes); err != nil {
		t.Fatalf("Failed to patch service: %s", err.Error())
	}

	afterCh := time.After(5 * time.Second)

	// check subroutes are private
	if err = v1a1test.WaitForRouteState(clients.ServingAlphaClient, resources.Route.Name, func(r *v1alpha1.Route) (bool, error) {
		for _, tag := range []string{subrouteTag1, subrouteTag2} {
			if isClusterLocal, err := isTrafficClusterLocal(r.Status.Traffic, tag); err != nil {
				return false, err
			} else if !isClusterLocal {
				return false, fmt.Errorf("Expected sub route with tag %s to be cluster local", tag)
			}
		}
		select {
		// consistently check for subroutes to be cluster-local for 5s
		case <-afterCh:
			return true, nil
		default:
			return false, nil
		}
	}, "sub routes are not ready"); err != nil {
		t.Fatalf("Expected sub routes are not cluster local: %s", err.Error())
	}

	// check route is private
	if !isRouteClusterLocal(resources.Route.Status) {
		t.Fatalf("Expected route to be cluster local")
	}

	// change route - public (Updating ksvc as it will reconcile the route)
	// check route = public
	ksvclabelCopy := resources.Service.DeepCopy()
	labels.SetVisibility(&ksvclabelCopy.ObjectMeta, false)
	if _, err = v1a1test.PatchService(t, clients, resources.Service, ksvclabelCopy); err != nil {
		t.Fatalf("Failed to patch service: %s", err.Error())
	}

	if err = v1a1test.WaitForRouteState(clients.ServingAlphaClient, resources.Route.Name, func(r *v1alpha1.Route) (b bool, e error) {
		return !isRouteClusterLocal(r.Status), nil
	}, "Route is public"); err != nil {
		t.Fatalf("Route is not public: %s", err.Error())
	}

	publicRoute, err := clients.ServingAlphaClient.Routes.Get(resources.Route.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf(err.Error())
	}

	//Check subroute 2 is public.
	if isClusterLocal, err := isTrafficClusterLocal(publicRoute.Status.Traffic, subrouteTag2); err != nil {
		t.Fatalf(err.Error())
	} else if isClusterLocal {
		t.Fatalf("Expected subroute with tag %s to be not cluster local", subrouteTag2)
	}

	//Check subroute1 is  private. This check is expected to fail on v0.8.1 and earlier as subroute1 becomes public)
	if isClusterLocal, err := isTrafficClusterLocal(publicRoute.Status.Traffic, subrouteTag1); err != nil {
		t.Fatalf(err.Error())
	} else if !isClusterLocal {
		t.Fatalf("Expected subroute with tag %s to be cluster local", subrouteTag1)
	}

	//Update and check subroute 1 to private.
	serviceName1 := serviceNameForRoute(subrouteTag1, resources.Route.Name)
	svc1, err := clients.KubeClient.Kube.CoreV1().Services(test.ServingNamespace).Get(serviceName1, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get k8s service to modify: %w", err)
	}

	svc1Copy := svc1.DeepCopy()
	labels.SetVisibility(&svcCopy.ObjectMeta, true)

	svc1patchBytes, err := test.CreateBytePatch(svc1, svc1Copy)
	if err != nil {
		t.Fatalf(err.Error())
	}

	if _, err = clients.KubeClient.Kube.CoreV1().Services(test.ServingNamespace).Patch(serviceName1, types.JSONPatchType, svc1patchBytes); err != nil {
		t.Fatalf("Failed to patch service: %w", err)
	}

	if err = v1a1test.WaitForRouteState(clients.ServingAlphaClient, resources.Route.Name, v1a1test.IsRouteReady, "Route is ready"); err != nil {
		t.Fatalf("Route did not become ready: %w", err)
	}

	if isClusterLocal, err := isTrafficClusterLocal(publicRoute.Status.Traffic, subrouteTag1); err != nil {
		t.Fatalf(err.Error())
	} else if !isClusterLocal {
		t.Fatalf("Expected subroute with tag %s to be cluster local", subrouteTag1)
	}
}

// Function check whether traffic with tag is cluster local or
func isTrafficClusterLocal(tt []v1alpha1.TrafficTarget, tag string) (bool, error) {
	for _, traffic := range tt {
		if traffic.Tag == tag {
			return strings.HasSuffix(traffic.TrafficTarget.URL.Host, network.GetClusterDomainName()), nil
		}
	}
	return false, errors.Errorf("Unable to find traffic target with tag %s", tag)
}

func isRouteClusterLocal(rs v1alpha1.RouteStatus) bool {
	return strings.HasSuffix(rs.URL.Host, network.GetClusterDomainName())
}

func serviceNameForRoute(subrouteTag, routeName string) string {
	return fmt.Sprintf("%s-%s", subrouteTag, routeName)
}
