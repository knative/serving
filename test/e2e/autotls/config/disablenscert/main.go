package disablenscert

import (
	"log"

	"github.com/kelseyhightower/envconfig"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/test"
)

type config struct {
	NamespaceWithCert string `envconfig:"namespace_with_cert" required: "false"`
}

var env config

func main() {
	if err := envconfig.Process("", &env); err != nil {
		log.Fatalf("Failed to process environment variable: %v.", err)
	}

	cfg, err := sharedmain.GetConfig("", "")
	if err != nil {
		log.Fatalf("Failed to build config: %v", err)
	}
	clients, err := test.NewClientsFromConfig(cfg, test.ServingNamespace)
	if err != nil {
		log.Fatalf("Failed to get test clients: %v", err)
	}
	whiteLists := sets.String{}
	if len(env.NamespaceWithCert) != 0 {
		whiteLists.Insert(env.NamespaceWithCert)
	}
	if err := disableNamespaceCertWithWhiteList(clients, whiteLists); err != nil {
		log.Fatalf("Failed to disable namespace cert: %v", err)
	}
}

func disableNamespaceCertWithWhiteList(clients *test.Clients, whiteLists sets.String) error {
	namespaces, err := clients.KubeClient.Kube.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, ns := range namespaces.Items {
		if ns.Labels == nil {
			ns.Labels = map[string]string{}
		}
		if whiteLists.Has(ns.Name) {
			delete(ns.Labels, networking.DisableWildcardCertLabelKey)
		} else {
			ns.Labels[networking.DisableWildcardCertLabelKey] = "true"
		}
		if _, err := clients.KubeClient.Kube.CoreV1().Namespaces().Update(&ns); err != nil {
			return err
		}
	}
	return nil
}
