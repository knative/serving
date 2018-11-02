package e2e

import (
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
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

// TearDownByYaml will delete resouce defined in yaml.
func TearDownByYaml(yamlFilename string, logger *logging.BaseLogger) {
	if yamlFilename != "" {
		exec.Command("kubectl", "delete", "-f", yamlFilename).Run()
		os.Remove(yamlFilename)
	}
}

// CreateConfigAndRouteFromYaml will create Route and Config by yaml file.
func CreateConfigAndRouteFromYaml(
	logger *logging.BaseLogger,
	imageName, appYaml string,
	configNamePlaceholder, routeNamePlaceholder, yamlImagePlaceholder, namespacePlaceholder string,
) (string, test.ResourceNames, error) {
	logger.Infof("Creating manifest")
	imagePath := test.ImagePath(imageName)

	var names test.ResourceNames
	names.Config = test.AppendRandomString(configName, logger)
	names.Route = test.AppendRandomString(routeName, logger)

	// Create manifest file.
	newYaml, err := ioutil.TempFile("", "TempYaml")
	if err != nil {
		logger.Errorf("Failed to create temporary manifest: %v", err)
		return "", names, err
	}
	newYamlFilename := newYaml.Name()

	// Populate manifets file with the real path to the container
	yamlBytes, err := ioutil.ReadFile(appYaml)
	if err != nil {
		logger.Errorf("Failed to read file %s: %v", appYaml, err)
		return newYamlFilename, names, err
	}

	content := strings.Replace(string(yamlBytes), yamlImagePlaceholder, imagePath, -1)
	content = strings.Replace(string(content), configNamePlaceholder, names.Config, -1)
	content = strings.Replace(string(content), routeNamePlaceholder, names.Route, -1)
	content = strings.Replace(string(content), namespacePlaceholder, test.ServingNamespace, -1)

	if _, err = newYaml.WriteString(content); err != nil {
		logger.Errorf("Failed to write new manifest: %v", err)
		return newYamlFilename, names, err
	}
	if err = newYaml.Close(); err != nil {
		logger.Errorf("Failed to close new manifest file: %v", err)
		return newYamlFilename, names, err
	}

	logger.Infof("Manifest file is '%s'", newYamlFilename)
	logger.Info("Deploying using kubectl")

	// Deploy using kubectl
	if output, err := exec.Command("kubectl", "apply", "-f", newYamlFilename).CombinedOutput(); err != nil {
		logger.Errorf("Error running kubectl: %v", strings.TrimSpace(string(output)))
		return newYamlFilename, names, err
	}

	return newYamlFilename, names, nil
}
