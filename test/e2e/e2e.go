package e2e

import (
	"testing"

	"go.uber.org/zap"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/knative/serving/test"
)

const (
	configName           = "prod"
	routeName            = "noodleburg"
	defaultNamespaceName = "serving-tests"
)

// Setup creates the client objects needed in the e2e tests.
func Setup(t *testing.T) *test.Clients {
	if test.Flags.Namespace == "" {
		test.Flags.Namespace = defaultNamespaceName
	}

	clients, err := test.NewClients(
		test.Flags.Kubeconfig,
		test.Flags.Cluster,
		test.Flags.Namespace)
	if err != nil {
		t.Fatalf("Couldn't initialize clients: %v", err)
	}
	return clients
}

// TearDown will delete created names using clients.
func TearDown(clients *test.Clients, names test.ResourceNames, logger *zap.SugaredLogger) {
	if clients != nil {
		clients.Delete([]string{names.Route}, []string{names.Config})
	}
}

// CreateRouteAndConfig will create Route and Config objects using clients.
// The Config object will serve requests to a container started from the image at imagePath.
func CreateRouteAndConfig(clients *test.Clients, logger *zap.SugaredLogger, imagePath string) (test.ResourceNames, error) {
	var names test.ResourceNames
	names.Config = test.AppendRandomString(configName, logger)
	names.Route = test.AppendRandomString(routeName, logger)

	_, err := clients.Configs.Create(
		test.Configuration(test.Flags.Namespace, names, imagePath))
	if err != nil {
		return test.ResourceNames{}, err
	}
	_, err = clients.Routes.Create(
		test.Route(test.Flags.Namespace, names))
	return names, err
}
