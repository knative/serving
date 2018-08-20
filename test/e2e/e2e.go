package e2e

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/knative/serving/test"
	"github.com/knative/serving/test/logging"
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
func TearDown(clients *test.Clients, names test.ResourceNames, logger *logging.BaseLogger) {
	if clients != nil && clients.ServingClient != nil {
		clients.ServingClient.Delete([]string{names.Route}, []string{names.Config}, []string{names.Service})
	}
}

// CreateRouteAndConfig will create Route and Config objects using clients.
// The Config object will serve requests to a container started from the image at imagePath.
func CreateRouteAndConfig(clients *test.Clients, logger *logging.BaseLogger, imagePath string) (test.ResourceNames, error) {
	return CreateRouteAndConfigWithEnv(clients, logger, imagePath, nil)
}

// CreateRouteAndConfigWithEnv will create Route and Config objects using clients.
// The Config object will serve requests to a container started from the image at imagePath and configured with given environment variables.
func CreateRouteAndConfigWithEnv(clients *test.Clients, logger *logging.BaseLogger, imagePath string, envVars []corev1.EnvVar) (test.ResourceNames, error) {
	var names test.ResourceNames
	names.Config = test.AppendRandomString(configName, logger)
	names.Route = test.AppendRandomString(routeName, logger)

	if err := test.CreateConfigurationWithEnv(logger, clients, names, imagePath, envVars); err != nil {
		return test.ResourceNames{}, err
	}
	err := test.CreateRoute(logger, clients, names)
	return names, err
}
