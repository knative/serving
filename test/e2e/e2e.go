package e2e

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/test"
	v1a1test "github.com/knative/serving/test/v1alpha1"
	perrors "github.com/pkg/errors"
)

// Setup creates the client objects needed in the e2e tests.
func Setup(t *testing.T) *test.Clients {
	return SetupWithNamespace(t, test.ServingNamespace)
}

// SetupAlternativeNamespace creates the client objects needed in e2e tests
// under the alternative namespace.
func SetupAlternativeNamespace(t *testing.T) *test.Clients {
	return SetupWithNamespace(t, test.AlternativeServingNamespace)
}

// SetupWithNamespace creates the client objects needed in the e2e tests under the specified namespace.
func SetupWithNamespace(t *testing.T, namespace string) *test.Clients {
	clients, err := test.NewClients(
		pkgTest.Flags.Kubeconfig,
		pkgTest.Flags.Cluster,
		namespace)
	if err != nil {
		t.Fatalf("Couldn't initialize clients: %v", err)
	}
	return clients
}

// CreateRouteAndConfig will create Route and Config objects using clients.
// The Config object will serve requests to a container started from the image at imagePath.
func CreateRouteAndConfig(t *testing.T, clients *test.Clients, image string, options *v1a1test.Options) (test.ResourceNames, error) {
	svcName := test.ObjectNameForTest(t)
	names := test.ResourceNames{
		Config: svcName,
		Route:  svcName,
		Image:  image,
	}

	if _, err := v1a1test.CreateConfiguration(t, clients, names, options); err != nil {
		return test.ResourceNames{}, err
	}
	_, err := v1a1test.CreateRoute(t, clients, names)
	return names, err
}

// WaitForScaleToZero will wait for the specified deployment to scale to 0 replicas.
// Will wait up to 6 times the configured ScaleToZeroGracePeriod before failing.
func WaitForScaleToZero(t *testing.T, deploymentName string, clients *test.Clients) error {
	t.Helper()
	t.Logf("Waiting for %q to scale to zero", deploymentName)

	// Assume an empty map (and therefore return defaults) if getting the config map fails.
	cmData := make(map[string]string)
	if autoscalerCM, err := clients.KubeClient.Kube.CoreV1().ConfigMaps("knative-serving").Get(autoscaler.ConfigName, metav1.GetOptions{}); err == nil {
		cmData = autoscalerCM.Data
	}

	config, err := autoscaler.NewConfigFromMap(cmData)
	if err != nil {
		return perrors.Wrap(err, "failed to parse configmap")
	}

	return pkgTest.WaitForDeploymentState(
		clients.KubeClient,
		deploymentName,
		test.DeploymentScaledToZeroFunc,
		"DeploymentIsScaledDown",
		test.ServingNamespace,
		config.ScaleToZeroGracePeriod*6,
	)
}
