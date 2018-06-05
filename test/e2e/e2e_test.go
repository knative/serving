package e2e

import (
	"testing"

	"github.com/knative/serving/test"
	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	NamespaceName = "noodleburg"
	ConfigName    = "prod"
	RouteName     = "noodleburg"
	IngressName   = RouteName + "-ingress"
)

func Setup(t *testing.T) *test.Clients {
	clients, err := test.NewClients(
		test.Flags.Kubeconfig,
		test.Flags.Cluster,
		NamespaceName)
	if err != nil {
		t.Fatalf("Couldn't initialize clients: %v", err)
	}
	return clients
}

func TearDown(clients *test.Clients) {
	if clients != nil {
		clients.Delete([]string{RouteName}, []string{ConfigName})
	}
}

func CreateRouteAndConfig(clients *test.Clients, imagePath string) error {
	_, err := clients.Configs.Create(
		test.Configuration(NamespaceName, ConfigName, imagePath))
	if err != nil {
		return err
	}
	_, err = clients.Routes.Create(
		test.Route(NamespaceName, RouteName, ConfigName))
	return err
}
