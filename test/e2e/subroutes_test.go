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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	netpkg "knative.dev/networking/pkg"
	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// In this test, we set up two apps: helloworld and httpproxy.
// helloworld is a simple app that displays a plaintext string with private visibility.
// httpproxy is a proxy that redirects request to internal service of helloworld app
// with {tag}-{route}.{namespace}.svc.cluster.local, or {tag}-{route}.{namespace}.svc, or {tag}-{route}.{namespace}.
// The expected result is that the request sent to httpproxy app is successfully redirected
// to helloworld app when trying to communicate via local address only.
func TestSubrouteLocalSTS(t *testing.T) { // We can't use a longer more descriptive name because routes will fail DNS checks. (Max 64 characters)
	t.Parallel()

	clients := Setup(t)

	t.Log("Creating a Service for the helloworld test app.")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.EnsureTearDown(t, clients, &names)

	tag := "current"

	withInternalVisibility := rtesting.WithServiceLabel(netpkg.VisibilityLabelKey, serving.VisibilityClusterLocal)
	withTrafficSpec := rtesting.WithRouteSpec(v1.RouteSpec{
		Traffic: []v1.TrafficTarget{
			{
				Tag:     tag,
				Percent: ptr.Int64(100),
			},
		},
	})

	resources, err := v1test.CreateServiceReady(t, clients, &names, withInternalVisibility, withTrafficSpec)
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

	clients := Setup(t)

	t.Log("Creating a Service for the helloworld test app.")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.EnsureTearDown(t, clients, &names)

	subrouteTag1 := "my-tag"
	subrouteTag2 := "my-tag2"

	withTrafficSpec := rtesting.WithRouteSpec(v1.RouteSpec{
		Traffic: []v1.TrafficTarget{{
			Tag:     subrouteTag1,
			Percent: ptr.Int64(100),
		}},
	})
	resources, err := v1test.CreateServiceReady(t, clients, &names, withTrafficSpec)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
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
		t.Fatal("Failed to get k8s service to modify:", err)
	}

	svcCopy := svc.DeepCopy()
	labels.SetVisibility(&svcCopy.ObjectMeta, true)

	svcpatchBytes, err := duck.CreateBytePatch(svc, svcCopy)
	if err != nil {
		t.Fatalf(err.Error())
	}

	if _, err = clients.KubeClient.Kube.CoreV1().Services(test.ServingNamespace).Patch(serviceName, types.JSONPatchType, svcpatchBytes); err != nil {
		t.Fatal("Failed to patch service:", err)
	}

	//Create subroute2 in kservice.
	ksvcCopy := resources.Service.DeepCopy()
	ksvcCopyRouteTraffic := append(ksvcCopy.Spec.Traffic,
		v1.TrafficTarget{
			Tag:            subrouteTag2,
			LatestRevision: ptr.Bool(true),
		})

	if _, err = v1test.UpdateServiceRouteSpec(t, clients, names, v1.RouteSpec{Traffic: ksvcCopyRouteTraffic}); err != nil {
		t.Fatal("Failed to patch service:", err)
	}

	if err = v1test.WaitForRouteState(clients.ServingClient, resources.Route.Name, func(r *v1.Route) (bool, error) {
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
		t.Fatalf("Expected subroute1 with tag %s to be not cluster local; subroute2 with tag %s to be cluster local: %v", subrouteTag1, subrouteTag2, err)
	}

	//Update route to private.
	if _, err = v1test.PatchService(t, clients, resources.Service, func(s *v1.Service) {
		labels.SetVisibility(&s.ObjectMeta, true)
	}); err != nil {
		t.Fatalf("Failed to patch service: %s", err.Error())
	}

	if err = v1test.WaitForRouteState(clients.ServingClient, resources.Route.Name, func(r *v1.Route) (bool, error) {
		return isRouteClusterLocal(r.Status), nil
	}, "Route is cluster local"); err != nil {
		t.Fatalf("Route did not become cluster local: %s", err.Error())
	}

	clusterLocalRoute, err := clients.ServingClient.Routes.Get(resources.Route.Name, metav1.GetOptions{})
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

	clients := Setup(t)

	t.Log("Creating a Service for the helloworld test app.")
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.EnsureTearDown(t, clients, &names)

	subrouteTag1 := "my-tag"
	subrouteTag2 := "my-tag2"

	withInternalVisibility := rtesting.WithServiceLabel(netpkg.VisibilityLabelKey, serving.VisibilityClusterLocal)
	withTrafficSpec := rtesting.WithRouteSpec(v1.RouteSpec{
		Traffic: []v1.TrafficTarget{
			{
				Tag:     subrouteTag1,
				Percent: ptr.Int64(50),
			},
			{
				Tag:     subrouteTag2,
				Percent: ptr.Int64(50),
			},
		},
	})
	resources, err := v1test.CreateServiceReady(t, clients, &names, withTrafficSpec, withInternalVisibility)
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

	svcpatchBytes, err := duck.CreateBytePatch(svc, svcCopy)
	if err != nil {
		t.Fatalf(err.Error())
	}

	if _, err = clients.KubeClient.Kube.CoreV1().Services(test.ServingNamespace).Patch(serviceName, types.JSONPatchType, svcpatchBytes); err != nil {
		t.Fatalf("Failed to patch service: %s", err.Error())
	}

	afterCh := time.After(5 * time.Second)

	// check subroutes are private
	if err = v1test.WaitForRouteState(clients.ServingClient, resources.Route.Name, func(r *v1.Route) (bool, error) {
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
	if _, err = v1test.PatchService(t, clients, resources.Service, func(s *v1.Service) {
		labels.SetVisibility(&s.ObjectMeta, false)
	}); err != nil {
		t.Fatalf("Failed to patch service: %s", err.Error())
	}

	if err = v1test.WaitForRouteState(clients.ServingClient, resources.Route.Name, func(r *v1.Route) (b bool, e error) {
		return !isRouteClusterLocal(r.Status), nil
	}, "Route is public"); err != nil {
		t.Fatalf("Route is not public: %s", err.Error())
	}

	publicRoute, err := clients.ServingClient.Routes.Get(resources.Route.Name, metav1.GetOptions{})
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
		t.Fatal("Failed to get k8s service to modify:", err)
	}

	svc1Copy := svc1.DeepCopy()
	labels.SetVisibility(&svcCopy.ObjectMeta, true)

	svc1patchBytes, err := duck.CreateBytePatch(svc1, svc1Copy)
	if err != nil {
		t.Fatalf(err.Error())
	}

	if _, err = clients.KubeClient.Kube.CoreV1().Services(test.ServingNamespace).Patch(serviceName1, types.JSONPatchType, svc1patchBytes); err != nil {
		t.Fatal("Failed to patch service:", err)
	}

	if err = v1test.WaitForRouteState(clients.ServingClient, resources.Route.Name, v1test.IsRouteReady, "Route is ready"); err != nil {
		t.Fatal("Route did not become ready:", err)
	}

	if isClusterLocal, err := isTrafficClusterLocal(publicRoute.Status.Traffic, subrouteTag1); err != nil {
		t.Fatalf(err.Error())
	} else if !isClusterLocal {
		t.Fatalf("Expected subroute with tag %s to be cluster local", subrouteTag1)
	}
}

// Function check whether traffic with tag is cluster local or
func isTrafficClusterLocal(tt []v1.TrafficTarget, tag string) (bool, error) {
	for _, traffic := range tt {
		if traffic.Tag == tag {
			return strings.HasSuffix(traffic.URL.Host, network.GetClusterDomainName()), nil
		}
	}
	return false, fmt.Errorf("Unable to find traffic target with tag %s", tag)
}

func isRouteClusterLocal(rs v1.RouteStatus) bool {
	return strings.HasSuffix(rs.URL.Host, network.GetClusterDomainName())
}

func serviceNameForRoute(subrouteTag, routeName string) string {
	return fmt.Sprintf("%s-%s", subrouteTag, routeName)
}
