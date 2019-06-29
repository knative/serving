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
	"github.com/knative/pkg/test"
	"github.com/knative/serving/pkg/client/clientset/versioned"
	servingv1alpha1 "github.com/knative/serving/pkg/client/clientset/versioned/typed/serving/v1alpha1"
	servingv1beta1 "github.com/knative/serving/pkg/client/clientset/versioned/typed/serving/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Clients holds instances of interfaces for making requests to Knative Serving.
type Clients struct {
	KubeClient         *test.KubeClient
	ServingAlphaClient *ServingAlphaClients
	ServingBetaClient  *ServingBetaClients
	Dynamic            dynamic.Interface
}

// ServingAlphaClients holds instances of interfaces for making requests to knative serving clients
type ServingAlphaClients struct {
	Routes    servingv1alpha1.RouteInterface
	Configs   servingv1alpha1.ConfigurationInterface
	Revisions servingv1alpha1.RevisionInterface
	Services  servingv1alpha1.ServiceInterface
}

// ServingBetaClients holds instances of interfaces for making requests to knative serving clients
type ServingBetaClients struct {
	Routes    servingv1beta1.RouteInterface
	Configs   servingv1beta1.ConfigurationInterface
	Revisions servingv1beta1.RevisionInterface
	Services  servingv1beta1.ServiceInterface
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

	// We poll, so set our limits high.
	cfg.QPS = 100
	cfg.Burst = 200

	clients.KubeClient, err = test.NewKubeClient(configPath, clusterName)
	if err != nil {
		return nil, err
	}

	clients.ServingAlphaClient, err = newServingAlphaClients(cfg, namespace)
	if err != nil {
		return nil, err
	}

	clients.ServingBetaClient, err = newServingBetaClients(cfg, namespace)
	if err != nil {
		return nil, err
	}

	clients.Dynamic, err = dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return clients, nil
}

// NewServingAlphaClients instantiates and returns the serving clientset required to make requests to the
// knative serving cluster.
func newServingAlphaClients(cfg *rest.Config, namespace string) (*ServingAlphaClients, error) {
	cs, err := versioned.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &ServingAlphaClients{
		Configs:   cs.ServingV1alpha1().Configurations(namespace),
		Revisions: cs.ServingV1alpha1().Revisions(namespace),
		Routes:    cs.ServingV1alpha1().Routes(namespace),
		Services:  cs.ServingV1alpha1().Services(namespace),
	}, nil
}

// NewServingBetaClients instantiates and returns the serving clientset required to make requests to the
// knative serving cluster.
func newServingBetaClients(cfg *rest.Config, namespace string) (*ServingBetaClients, error) {
	cs, err := versioned.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &ServingBetaClients{
		Configs:   cs.ServingV1beta1().Configurations(namespace),
		Revisions: cs.ServingV1beta1().Revisions(namespace),
		Routes:    cs.ServingV1beta1().Routes(namespace),
		Services:  cs.ServingV1beta1().Services(namespace),
	}, nil
}

// Delete will delete all Routes and Configs with the names routes and configs, if clients
// has been successfully initialized.
func (clients *ServingAlphaClients) Delete(routes []string, configs []string, services []string) error {
	deletions := []struct {
		client interface {
			Delete(name string, options *v1.DeleteOptions) error
		}
		items []string
	}{
		{clients.Routes, routes},
		{clients.Configs, configs},
		{clients.Services, services},
	}

	propPolicy := v1.DeletePropagationForeground
	dopt := &v1.DeleteOptions{
		PropagationPolicy: &propPolicy,
	}

	for _, deletion := range deletions {
		if deletion.client == nil {
			continue
		}

		for _, item := range deletion.items {
			if item == "" {
				continue
			}

			if err := deletion.client.Delete(item, dopt); err != nil {
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
