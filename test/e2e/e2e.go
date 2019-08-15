package e2e

import (
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	perrors "github.com/pkg/errors"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/autoscaler"
	"knative.dev/serving/test"
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

// autoscalerCM returns the current autoscaler config map deployed to the
// test cluster.
func autoscalerCM(clients *test.Clients) (*autoscaler.Config, error) {
	autoscalerCM, err := clients.KubeClient.Kube.CoreV1().ConfigMaps("knative-serving").Get(
		autoscaler.ConfigName,
		metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return autoscaler.NewConfigFromMap(autoscalerCM.Data)
}

// WaitForScaleToZero will wait for the specified deployment to scale to 0 replicas.
// Will wait up to 6 times the configured ScaleToZeroGracePeriod before failing.
func WaitForScaleToZero(t *testing.T, deploymentName string, clients *test.Clients) error {
	t.Helper()

	return WaitForScaleToN(t, deploymentName, clients, 0)
}

// WaitForScaleToN will wait for the specified deployment to scale to N replicas.
// Will wait up to 6 times the configured ScaleToZeroGracePeriod before failing.
func WaitForScaleToN(t *testing.T, deploymentName string, clients *test.Clients, n int32) error {
	t.Helper()
	t.Logf("Waiting for %q to scale to %d", deploymentName, n)

	cfg, err := autoscalerCM(clients)
	if err != nil {
		return perrors.Wrap(err, "failed to get autoscaler configmap")
	}

	return pkgTest.WaitForDeploymentState(
		clients.KubeClient,
		deploymentName,
		func(d *appsv1.Deployment) (bool, error) {
			return d.Status.ReadyReplicas == n, nil
		},
		fmt.Sprintf("DeploymentIsScaledTo%d", n),
		test.ServingNamespace,
		cfg.ScaleToZeroGracePeriod*6,
	)
}
