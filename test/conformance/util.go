package conformance

import (
	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/serving/test"
	"testing"

	// Mysteriously required to support GCP auth (required by k8s libs). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

// Constants for test images located in test/test_images
const (
	pizzaPlanet1 = "pizzaplanetv1"
	pizzaPlanet2 = "pizzaplanetv2"
	helloworld   = "helloworld"
	httpproxy    = "httpproxy"
)

func setup(t *testing.T) *test.Clients {
	clients, err := test.NewClients(pkgTest.Flags.Kubeconfig, pkgTest.Flags.Cluster, test.ServingNamespace)
	if err != nil {
		t.Fatalf("Couldn't initialize clients: %v", err)
	}
	return clients
}

func tearDown(clients *test.Clients, names test.ResourceNames) {
	if clients != nil && clients.ServingClient != nil {
		clients.ServingClient.Delete([]string{names.Route}, []string{names.Config}, []string{names.Service})
	}
}
