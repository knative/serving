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
	"k8s.io/client-go/kubernetes"

	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/serving/pkg/apis/networking"
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
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("Failed to create kube client: %v", err)
	}
	whiteLists := sets.String{}
	if len(env.NamespaceWithCert) != 0 {
		whiteLists.Insert(env.NamespaceWithCert)
	}
	if err := disableNamespaceCertWithWhiteList(kubeClient, whiteLists); err != nil {
		log.Fatalf("Failed to disable namespace cert: %v", err)
	}
}

func disableNamespaceCertWithWhiteList(kubeClient *kubernetes.Clientset, whiteLists sets.String) error {
	namespaces, err := kubeClient.CoreV1().Namespaces().List(metav1.ListOptions{})
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
		if _, err := kubeClient.CoreV1().Namespaces().Update(&ns); err != nil {
			return err
		}
	}
	return nil
}
