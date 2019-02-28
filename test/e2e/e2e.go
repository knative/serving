package e2e

import (
	"testing"
	"time"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	"k8s.io/api/extensions/v1beta1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/serving/test"
)

const (
	configName               = "prod"
	routeName                = "noodleburg"
	helloWorldExpectedOutput = "Hello World! How about some tasty noodles?"
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

// CreateRouteAndConfig will create Route and Config objects using clients.
// The Config object will serve requests to a container started from the image at imagePath.
func CreateRouteAndConfig(t *testing.T, clients *test.Clients, image string, options *test.Options) (test.ResourceNames, error) {
	svcName := test.ObjectNameForTest(t)
	names := test.ResourceNames{
		Config: svcName,
		Route:  svcName,
		Image:  image,
	}

	if _, err := test.CreateConfiguration(t, clients, names, options); err != nil {
		return test.ResourceNames{}, err
	}
	_, err := test.CreateRoute(t, clients, names)
	return names, err
}

// WaitForScaleToZero will wait for the deployment specified by names to scale
// to 0 replicas. Will wait up to 3 minutes before failing.
func WaitForScaleToZero(t *testing.T, names test.ResourceNames, clients *test.Clients) {
	t.Helper()
	deploymentName := names.Revision + "-deployment"
	t.Logf("Waiting for %q to scale to zero", deploymentName)
	err := pkgTest.WaitForDeploymentState(
		clients.KubeClient,
		deploymentName,
		func(d *v1beta1.Deployment) (bool, error) {
			t.Logf("Deployment %q has %d replicas", deploymentName, d.Status.ReadyReplicas)
			return d.Status.ReadyReplicas == 0, nil
		},
		"DeploymentIsScaledDown",
		test.ServingNamespace,
		3*time.Minute,
	)
	if err != nil {
		t.Fatalf("Could not scale to zero: %v", err)
	}
}
