/*
Copyright 2018 The Knative Authors

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
	// The set of controllers this controller process runs.
	"flag"
	"log"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"

	// This defines the shared main for injected controllers.

	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/signals"
	"knative.dev/serving/pkg/apis/serving"
	servingscheme "knative.dev/serving/pkg/client/serving/clientset/internalversion/scheme"
	"knative.dev/serving/pkg/reconciler/service"
)

func main() {
	var (
		masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
		kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	)
	flag.Parse()

	cfg, err := sharedmain.GetConfig(*masterURL, *kubeconfig)
	if err != nil {
		log.Fatal("Error building kubeconfig", err)
	}

	dclient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		log.Fatal("Error discovering serving APIs", err)
	}

	// TODO(dprotaso) We'd normally do this for the entire API group and
	// not a single resource.
	// For demo purposes swap the service version based on what's available
	resourceList, err := discovery.ServerPreferredResources(dclient) // ([]*metav1.APIResourceList, error) {

	if err != nil {
		log.Panicf("error fetching k8s resources: %v", err)
	}

	var kServiceAPIVersion string

outer:
	for _, list := range resourceList {
		gv, _ := schema.ParseGroupVersion(list.GroupVersion)
		if gv.Group != serving.GroupName {
			continue
		}

		for _, r := range list.APIResources {
			if r.Kind == "Service" {
				kServiceAPIVersion = gv.Version
				servingscheme.Scheme.SetVersionPriority(gv)
				break outer
			}
		}
	}
	if kServiceAPIVersion == "" {
		panic("Unable to determine Knative Service API Version")
	}

	log.Printf("Using Service version: %v", kServiceAPIVersion)

	sharedmain.MainWithConfig(
		signals.NewContext(),
		"controller-service",
		cfg,
		service.NewController,
	)
}
