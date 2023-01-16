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
	"knative.dev/pkg/test/logging"
	"knative.dev/pkg/test/logstream"

	// Mysteriously required to support GCP auth (required by k8s libs). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

// Constants for test images located in test/test_images.
const (
	// Test image names
	Autoscale           = "autoscale"
	Failing             = "failing"
	GRPCPing            = "grpc-ping"
	HelloHTTP2          = "hellohttp2"
	HelloVolume         = "hellovolume"
	HelloWorld          = "helloworld"
	HTTPProxy           = "httpproxy"
	InvalidHelloWorld   = "invalidhelloworld" // Not a real image
	PizzaPlanet1        = "pizzaplanetv1"
	PizzaPlanet2        = "pizzaplanetv2"
	Protocols           = "protocols"
	Readiness           = "readiness"
	Runtime             = "runtime"
	ServingContainer    = "servingcontainer"
	SidecarContainer    = "sidecarcontainer"
	SingleThreadedImage = "singlethreaded"
	Timeout             = "timeout"
	Volumes             = "volumes"
	WorkingDir          = "workingdir"

	// Constants for test image output.
	PizzaPlanetText1 = "What a spaceport!"
	PizzaPlanetText2 = "Re-energize yourself with a slice of pepperoni!"
	HelloWorldText   = "Hello World! How about some tasty noodles?"
	HelloHTTP2Text   = "Hello, New World! How about donuts and coffee?"
	EmptyDirText     = "From file in empty dir!"

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

type Options struct {
	Namespace        string
	DisableLogStream bool
}

// Setup creates client to run Knative Service requests
func Setup(t testing.TB, opts ...Options) *Clients {
	var o Options
	switch len(opts) {
	case 1:
		o = opts[0]
	case 0:
		o = Options{}
	default:
		t.Fatalf("multiple Options supplied to Setup")
	}

	t.Helper()
	logging.InitializeLogger()

	if !ServingFlags.DisableLogStream && !o.DisableLogStream {
		cancel := logstream.Start(t)
		t.Cleanup(cancel)
	}

	cfg, err := pkgTest.Flags.GetRESTConfig()
	if err != nil {
		t.Fatal("Couldn't get REST config", "error", err)
	}

	ns := ServingFlags.TestNamespace
	if len(o.Namespace) > 0 {
		ns = o.Namespace
	}

	clients, err := NewClients(cfg, ns)
	if err != nil {
		t.Fatal("Couldn't initialize clients", "error", err)
	}
	return clients
}
