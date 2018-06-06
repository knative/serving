package e2e

import (
	"flag"
	"testing"

	"github.com/knative/serving/test"
	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

// These are passed via:
//   go test -v ./test/... -args -namespace=knoodleburg -config=test -route=knoodler
var namespace = flag.String("namespace", "noodleburg", "the namespace in which to create resources.")
var configName = flag.String("config", "prod", "the name to give the config resource.")
var routeName = flag.String("route", "noodleburg", "the name to give the route resource.")

type Helper struct {
	*test.Clients

	NamespaceName string
	ConfigName    string
	RouteName     string
}

func Setup(t *testing.T) *Helper {
	clients, err := test.NewClients(
		test.Flags.Kubeconfig,
		test.Flags.Cluster,
		*namespace)
	if err != nil {
		t.Fatalf("Couldn't initialize clients: %v", err)
	}
	return &Helper{
		Clients:       clients,
		NamespaceName: *namespace,
		ConfigName:    *configName,
		RouteName:     *routeName,
	}
}

func (h *Helper) IngressName() string {
	return h.RouteName + "-ingress"
}

func (h *Helper) TearDown() {
	if h.Clients != nil {
		h.Clients.Delete([]string{h.RouteName}, []string{h.ConfigName})
	}
}

func (h *Helper) CreateRouteAndConfig(imagePath string) error {
	_, err := h.Clients.Configs.Create(
		test.Configuration(h.NamespaceName, h.ConfigName, imagePath))
	if err != nil {
		return err
	}
	_, err = h.Clients.Routes.Create(
		test.Route(h.NamespaceName, h.RouteName, h.ConfigName))
	return err
}
