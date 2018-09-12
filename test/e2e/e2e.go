package e2e

import (
	"testing"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"
)

const (
	configName = "prod"
	routeName  = "noodleburg"
)

// Setup creates the client objects needed in the e2e tests.
func Setup(t *testing.T) *test.Clients {
	clients, err := test.NewClients(
		pkgTest.Flags.Kubeconfig,
		pkgTest.Flags.Cluster,
		test.ServingNamespace)
	if err != nil {
		t.Fatalf("Couldn't initialize clients: %v", err)
	}
	return clients
}

// TearDown will delete created names using clients.
func TearDown(clients *test.Clients, names test.ResourceNames, logger *logging.BaseLogger) {
	if clients != nil && clients.ServingClient != nil {
		clients.ServingClient.Delete([]string{names.Route}, []string{names.Config}, []string{names.Service})
	}
}

// CreateRouteAndConfig will create Route and Config objects using clients.
// The Config object will serve requests to a container started from the image at imagePath.
func CreateRouteAndConfig(clients *test.Clients, logger *logging.BaseLogger, imagePath string, options *test.Options) (test.ResourceNames, error) {
	var names test.ResourceNames
	names.Config = test.AppendRandomString(configName, logger)
	names.Route = test.AppendRandomString(routeName, logger)

	if err := test.CreateConfiguration(logger, clients, names, imagePath, options); err != nil {
		return test.ResourceNames{}, err
	}
	err := test.CreateRoute(logger, clients, names)
	return names, err
}
