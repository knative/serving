/*
Copyright 2018 Google LLC

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
package v1alpha1

import (
	v1alpha1 "github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/client/clientset/versioned/scheme"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer"
	rest "k8s.io/client-go/rest"
)

type ElafrosV1alpha1Interface interface {
	RESTClient() rest.Interface
	ConfigurationsGetter
	RevisionsGetter
	RoutesGetter
}

// ElafrosV1alpha1Client is used to interact with features provided by the elafros.dev group.
type ElafrosV1alpha1Client struct {
	restClient rest.Interface
}

func (c *ElafrosV1alpha1Client) Configurations(namespace string) ConfigurationInterface {
	return newConfigurations(c, namespace)
}

func (c *ElafrosV1alpha1Client) Revisions(namespace string) RevisionInterface {
	return newRevisions(c, namespace)
}

func (c *ElafrosV1alpha1Client) Routes(namespace string) RouteInterface {
	return newRoutes(c, namespace)
}

// NewForConfig creates a new ElafrosV1alpha1Client for the given config.
func NewForConfig(c *rest.Config) (*ElafrosV1alpha1Client, error) {
	config := *c
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return &ElafrosV1alpha1Client{client}, nil
}

// NewForConfigOrDie creates a new ElafrosV1alpha1Client for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *ElafrosV1alpha1Client {
	client, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return client
}

// New creates a new ElafrosV1alpha1Client for the given RESTClient.
func New(c rest.Interface) *ElafrosV1alpha1Client {
	return &ElafrosV1alpha1Client{c}
}

func setConfigDefaults(config *rest.Config) error {
	gv := v1alpha1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/apis"
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	return nil
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *ElafrosV1alpha1Client) RESTClient() rest.Interface {
	if c == nil {
		return nil
	}
	return c.restClient
}
