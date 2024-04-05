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

// This file contains an object which encapsulates k8s clients which are useful for e2e tests.

package test

import (
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	// Allow E2E to run against a cluster using OpenID.
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	"knative.dev/networking/pkg/client/clientset/versioned"
	networkingv1alpha1 "knative.dev/networking/pkg/client/clientset/versioned/typed/networking/v1alpha1"
)

// Clients holds instances of interfaces for making requests to Knative Serving.
type Clients struct {
	KubeClient       kubernetes.Interface
	NetworkingClient *NetworkingClients
	Dynamic          dynamic.Interface
}

// NetworkingClients holds instances of interfaces for making requests to Knative
// networking clients.
type NetworkingClients struct {
	ServerlessServices networkingv1alpha1.ServerlessServiceInterface
	Ingresses          networkingv1alpha1.IngressInterface
	Certificates       networkingv1alpha1.CertificateInterface
}

// NewClientsFromConfig instantiates and returns several clientsets required for making request to the
// Knative Serving cluster specified by the combination of clusterName and configPath. Clients can
// make requests within namespace.
func NewClientsFromConfig(cfg *rest.Config, namespace string) (*Clients, error) {
	// We poll, so set our limits high.
	cfg.QPS = 100
	cfg.Burst = 200

	var (
		err     error
		clients Clients
	)

	clients.KubeClient, err = kubernetes.NewForConfig(cfg)
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
