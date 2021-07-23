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
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	// Allow E2E to run against a cluster using OpenID.
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	netclientset "knative.dev/networking/pkg/client/clientset/versioned"
	networkingv1alpha1 "knative.dev/networking/pkg/client/clientset/versioned/typed/networking/v1alpha1"
	"knative.dev/serving/pkg/client/clientset/versioned"
	servingv1 "knative.dev/serving/pkg/client/clientset/versioned/typed/serving/v1"
	servingv1alpha1 "knative.dev/serving/pkg/client/clientset/versioned/typed/serving/v1alpha1"
	servingv1beta1 "knative.dev/serving/pkg/client/clientset/versioned/typed/serving/v1beta1"

	// Every E2E test requires this, so add it here.
	_ "knative.dev/pkg/metrics/testing"
)

// Clients holds instances of interfaces for making requests to Knative Serving.
type Clients struct {
	KubeClient         kubernetes.Interface
	ServingAlphaClient *ServingAlphaClients
	ServingBetaClient  *ServingBetaClients
	ServingClient      *ServingClients
	NetworkingClient   *NetworkingClients
	Dynamic            dynamic.Interface
}

// ServingAlphaClients holds instances of interfaces for making requests to knative serving clients.
type ServingAlphaClients struct {
	DomainMappings servingv1alpha1.DomainMappingInterface
}

// ServingBetaClients holds instances of interfaces for making requests to knative serving clients.
type ServingBetaClients struct {
	DomainMappings servingv1beta1.DomainMappingInterface
}

// ServingClients holds instances of interfaces for making requests to knative serving clients.
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
// Knative Serving cluster specified by the rest Config. Clients can make requests within namespace.
func NewClients(cfg *rest.Config, namespace string) (*Clients, error) {
	// We poll, so set our limits high.
	cfg.QPS = 100
	cfg.Burst = 200

	var (
		clients Clients
		err     error
	)

	clients.KubeClient, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	clients.ServingClient, err = newServingClients(cfg, namespace)
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

	clients.NetworkingClient, err = newNetworkingClients(cfg, namespace)
	if err != nil {
		return nil, err
	}

	return &clients, nil
}

// newNetworkingClients instantiates and returns the networking clientset required to make requests
// to Networking resources on the Knative service cluster.
func newNetworkingClients(cfg *rest.Config, namespace string) (*NetworkingClients, error) {
	cs, err := netclientset.NewForConfig(cfg)
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
		DomainMappings: cs.ServingV1alpha1().DomainMappings(namespace),
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
		DomainMappings: cs.ServingV1beta1().DomainMappings(namespace),
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

// Delete will delete all Routes and Configs with the named routes and configs, if clients
// have been successfully initialized.
func (clients *ServingClients) Delete(routes, configs, services []string) []error {
	deletions := []struct {
		client interface {
			Delete(ctx context.Context, name string, options metav1.DeleteOptions) error
		}
		items []string
	}{
		// Delete services first, since we otherwise might delete a route/configuration
		// out from under the ksvc
		{clients.Services, services},
		{clients.Routes, routes},
		{clients.Configs, configs},
	}

	propPolicy := metav1.DeletePropagationForeground
	dopt := metav1.DeleteOptions{
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

			if err := deletion.client.Delete(context.Background(), item, dopt); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errs
}
