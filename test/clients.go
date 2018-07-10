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

// This file contains an object which encapsulates k8s clients which are useful for e2e tests.

package test

import (
	"github.com/knative/serving/pkg/client/clientset/versioned"
	servingtyped "github.com/knative/serving/pkg/client/clientset/versioned/typed/serving/v1alpha1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Clients holds instances of interfaces for making requests to Knative Serving.
type Clients struct {
	Kube      *kubernetes.Clientset
	Routes    servingtyped.RouteInterface
	Configs   servingtyped.ConfigurationInterface
	Revisions servingtyped.RevisionInterface
	Services  servingtyped.ServiceInterface
}

// NewClients instantiates and returns several clientsets required for making request to the
// Knative Serving cluster specified by the combination of clusterName and configPath. Clients can
// make requests within namespace.
func NewClients(configPath string, clusterName string, namespace string) (*Clients, error) {
	clients := &Clients{}
	cfg, err := buildClientConfig(configPath, clusterName)
	if err != nil {
		return nil, err
	}
	clients.Kube, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	cs, err := versioned.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	clients.Routes = cs.ServingV1alpha1().Routes(namespace)
	clients.Configs = cs.ServingV1alpha1().Configurations(namespace)
	clients.Revisions = cs.ServingV1alpha1().Revisions(namespace)
	clients.Services = cs.ServingV1alpha1().Services(namespace)

	return clients, nil
}

// Delete will delete all Routes and Configs with the names routes and configs, if clients
// has been successfully initialized.
func (clients *Clients) Delete(routes []string, configs []string) error {
	if clients.Routes != nil {
		for _, route := range routes {
			err := clients.Routes.Delete(route, nil)
			if err != nil {
				return err
			}
		}
	}

	if clients.Configs != nil {
		for _, config := range configs {
			err := clients.Configs.Delete(config, nil)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func buildClientConfig(kubeConfigPath string, clusterName string) (*rest.Config, error) {
	overrides := clientcmd.ConfigOverrides{}
	// Override the cluster name if provided.
	if clusterName != "" {
		overrides.Context.Cluster = clusterName
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfigPath},
		&overrides).ClientConfig()
}
