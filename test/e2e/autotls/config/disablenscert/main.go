/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"log"

	"github.com/kelseyhightower/envconfig"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/injection/sharedmain"
	test "knative.dev/serving/test"
)

type config struct {
	NamespaceWithCert string `envconfig:"namespace_with_cert" required:"false"`
}

var env config

func main() {
	if err := envconfig.Process("", &env); err != nil {
		log.Fatal("Failed to process environment variable: ", err)
	}

	cfg, err := sharedmain.GetConfig("", "")
	if err != nil {
		log.Fatal("Failed to build config: ", err)
	}
	clients, err := test.NewClientsFromConfig(cfg, test.ServingNamespace)
	if err != nil {
		log.Fatal("Failed to create clients: ", err)
	}
	whiteLists := sets.String{}
	if env.NamespaceWithCert != "" {
		whiteLists.Insert(env.NamespaceWithCert)
	}
	if err := disableNamespaceCertWithWhiteList(clients, whiteLists); err != nil {
		log.Fatal("Failed to disable namespace cert: ", err)
	}
}

func disableNamespaceCertWithWhiteList(clients *test.Clients, whiteLists sets.String) error {
	namespaces, err := clients.KubeClient.Kube.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, ns := range namespaces.Items {
		if whiteLists.Has(ns.Name) {
			delete(ns.Labels, networking.DisableWildcardCertLabelKey)
			delete(ns.Labels, networking.DeprecatedDisableWildcardCertLabelKey)
		} else {
			if ns.Labels == nil {
				ns.Labels = make(map[string]string, 1)
			}
			ns.Labels[networking.DisableWildcardCertLabelKey] = "true"
		}
		if _, err := clients.KubeClient.Kube.CoreV1().Namespaces().Update(&ns); err != nil {
			return err
		}
	}
	return nil
}
