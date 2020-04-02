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
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"knative.dev/pkg/test"
	"knative.dev/serving/pkg/client/clientset/versioned"
	networkingv1alpha1 "knative.dev/serving/pkg/client/clientset/versioned/typed/networking/v1alpha1"
	servingv1 "knative.dev/serving/pkg/client/clientset/versioned/typed/serving/v1"
	servingv1alpha1 "knative.dev/serving/pkg/client/clientset/versioned/typed/serving/v1alpha1"
	servingv1beta1 "knative.dev/serving/pkg/client/clientset/versioned/typed/serving/v1beta1"
	istioclientset "knative.dev/serving/pkg/client/istio/clientset/versioned"
)

// Clients holds instances of interfaces for making requests to Knative Serving.
type Clients struct {
	KubeClient         *test.KubeClient
	ServingAlphaClient *ServingAlphaClients
	ServingBetaClient  *ServingBetaClients
	ServingClient      *ServingClients
	NetworkingClient   *NetworkingClients
	Dynamic            dynamic.Interface
	IstioClient        istioclientset.Interface
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

// ServingClients holds instances of interfaces for making requests to knative serving clients
type ServingClients struct {
	Routes    servingv1.RouteInterface
	Configs   servingv1.ConfigurationInterface
	Revisions servingv1.RevisionInterface
	Services  servingv1.ServiceInterface
}

// NetworkingClients holds instances of interfaces for making requests to Knative
// networking clients.
type NetworkingClients struct {
	ServerlessServices networkingv1alpha1.ServerlessServiceInterface
	Ingresses          networkingv1alpha1.IngressInterface
	Certificates       networkingv1alpha1.CertificateInterface
}

// NewClients instantiates and returns several clientsets required for making request to the
// Knative Serving cluster specified by the combination of clusterName and configPath. Clients can
// make requests within namespace.
func NewClients(configPath string, clusterName string, namespace string) (*Clients, error) {
	cfg, err := BuildClientConfig(configPath, clusterName)
	if err != nil {
		return nil, err
	}

	// We poll, so set our limits high.
	cfg.QPS = 100
	cfg.Burst = 200

	return NewClientsFromConfig(cfg, namespace)
}

// NewClientsFromConfig instantiates and returns several clientsets required for making request to the
// Knative Serving cluster specified by the rest Config. Clients can make requests within namespace.
func NewClientsFromConfig(cfg *rest.Config, namespace string) (*Clients, error) {
	clients := &Clients{}
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	clients.KubeClient = &test.KubeClient{Kube: kubeClient}

	clients.ServingAlphaClient, err = newServingAlphaClients(cfg, namespace)
	if err != nil {
		return nil, err
	}

	clients.ServingBetaClient, err = newServingBetaClients(cfg, namespace)
	if err != nil {
		return nil, err
	}

	clients.ServingClient, err = newServingClients(cfg, namespace)
	if err != nil {
		return nil, err
	}

	clients.Dynamic, err = dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	clients.IstioClient, err = istioclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	clients.NetworkingClient, err = newNetworkingClients(cfg, namespace)
	if err != nil {
		return nil, err
	}

	return clients, nil
}

// newNetworkingClients instantiates and returns the networking clientset required to make requests
// to Networking resources on the Knative service cluster
func newNetworkingClients(cfg *rest.Config, namespace string) (*NetworkingClients, error) {
	cs, err := versioned.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &NetworkingClients{
		ServerlessServices: cs.NetworkingV1alpha1().ServerlessServices(namespace),
		Ingresses:          cs.NetworkingV1alpha1().Ingresses(namespace),
		Certificates:       cs.NetworkingV1alpha1().Certificates(namespace),
	}, nil
}

// newServingAlphaClients instantiates and returns the serving clientset required to make requests to the
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

// newServingBetaClients instantiates and returns the serving clientset required to make requests to the
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

// newServingClients instantiates and returns the serving clientset required to make requests to the
// knative serving cluster.
func newServingClients(cfg *rest.Config, namespace string) (*ServingClients, error) {
	cs, err := versioned.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &ServingClients{
		Configs:   cs.ServingV1().Configurations(namespace),
		Revisions: cs.ServingV1().Revisions(namespace),
		Routes:    cs.ServingV1().Routes(namespace),
		Services:  cs.ServingV1().Services(namespace),
	}, nil
}

// Delete will delete all Routes and Configs with the names routes and configs, if clients
// has been successfully initialized.
func (clients *ServingClients) Delete(routes []string, configs []string, services []string) []error {
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

	var errs []error
	for _, deletion := range deletions {
		if deletion.client == nil {
			continue
		}

		for _, item := range deletion.items {
			if item == "" {
				continue
			}

			if err := deletion.client.Delete(item, dopt); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errs
}

// BuildClientConfig builds client config for testing.
func BuildClientConfig(kubeConfigPath string, clusterName string) (*rest.Config, error) {
	overrides := clientcmd.ConfigOverrides{}
	// Override the cluster name if provided.
	if clusterName != "" {
		overrides.Context.Cluster = clusterName
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfigPath},
		&overrides).ClientConfig()
}
