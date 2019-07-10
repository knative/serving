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

	"knative.dev/pkg/test/logstream"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	routeconfig "github.com/knative/serving/pkg/reconciler/route/config"
	"github.com/knative/serving/test"
	v1a1test "github.com/knative/serving/test/v1alpha1"

	. "github.com/knative/serving/pkg/testing/v1alpha1"
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
				TrafficTarget: v1beta1.TrafficTarget{
					Tag:            tag,
					Percent:        100,
				},
			},
		},
	})

	resources, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, &v1a1test.Options{}, withInternalVisibility, withTrafficSpec)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	t.Logf("helloworld internal domain is %s.", resources.Route.Status.URL.Host)

	// helloworld app and its route are ready. Running the test cases now.
	for _, tc := range testCases {
		domain := fmt.Sprintf("%s-%s", tag, resources.Route.Status.Address.URL.Host)
		helloworldDomain := strings.TrimSuffix(domain, tc.suffix)
		t.Run(tc.name, func(t *testing.T) {
			testProxyToHelloworld(t, clients, helloworldDomain)
		})
	}
}
