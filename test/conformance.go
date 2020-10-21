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

package test

import (
	"testing"

	// For our e2e testing, we want this linked first so that our
	// systen namespace environment variable is defaulted prior to
	// logstream initialization.
	_ "knative.dev/serving/test/defaultsystem"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logstream"

	// Mysteriously required to support GCP auth (required by k8s libs). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

// Constants for test images located in test/test_images.
const (
	// Test image names
	Autoscale           = "autoscale"
	Failing             = "failing"
	HelloVolume         = "hellovolume"
	HelloWorld          = "helloworld"
	HTTPProxy           = "httpproxy"
	InvalidHelloWorld   = "invalidhelloworld" // Not a real image
	PizzaPlanet1        = "pizzaplanetv1"
	PizzaPlanet2        = "pizzaplanetv2"
	Protocols           = "protocols"
	Runtime             = "runtime"
	SingleThreadedImage = "singlethreaded"
	Timeout             = "timeout"
	WorkingDir          = "workingdir"
	ServingContainer    = "servingcontainer"
	SidecarContainer    = "sidecarcontainer"

	// Constants for test image output.
	PizzaPlanetText1 = "What a spaceport!"
	PizzaPlanetText2 = "Re-energize yourself with a slice of pepperoni!"
	HelloWorldText   = "Hello World! How about some tasty noodles?"
	HelloHTTP2Text   = "Hello, New World! How about donuts and coffee?"

	MultiContainerResponse = "Yay!! multi-container works"

	ConcurrentRequests = 200
	// We expect to see 100% of requests succeed for traffic sent directly to revisions.
	// This might be a bad assumption.
	MinDirectPercentage = 1
	// We expect to see at least 25% of either response since we're routing 50/50.
	// The CDF of the binomial distribution tells us this will flake roughly
	// 1 time out of 10^12 (roughly the number of galaxies in the observable universe).
	MinSplitPercentage = 0.25
)

// Setup creates client to run Knative Service requests
func Setup(t testing.TB) *Clients {
	t.Helper()

	cancel := logstream.Start(t)
	t.Cleanup(cancel)

	clients, err := NewClients(pkgTest.Flags.Kubeconfig, pkgTest.Flags.Cluster, ServingNamespace)
	if err != nil {
		t.Fatal("Couldn't initialize clients", "error", err.Error())
	}
	return clients
}
