/*
Copyright 2018 The Kubernetes Authors.

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
package versioned

import (
	cloudbuildv1alpha1 "github.com/elafros/elafros/pkg/client/clientset/versioned/typed/cloudbuild/v1alpha1"
	elafrosv1alpha1 "github.com/elafros/elafros/pkg/client/clientset/versioned/typed/ela/v1alpha1"
	configv1alpha2 "github.com/elafros/elafros/pkg/client/clientset/versioned/typed/istio/v1alpha2"
	glog "github.com/golang/glog"
	discovery "k8s.io/client-go/discovery"
	rest "k8s.io/client-go/rest"
	flowcontrol "k8s.io/client-go/util/flowcontrol"
)

type Interface interface {
	Discovery() discovery.DiscoveryInterface
	CloudbuildV1alpha1() cloudbuildv1alpha1.CloudbuildV1alpha1Interface
	// Deprecated: please explicitly pick a version if possible.
	Cloudbuild() cloudbuildv1alpha1.CloudbuildV1alpha1Interface
	ElafrosV1alpha1() elafrosv1alpha1.ElafrosV1alpha1Interface
	// Deprecated: please explicitly pick a version if possible.
	Elafros() elafrosv1alpha1.ElafrosV1alpha1Interface
	ConfigV1alpha2() configv1alpha2.ConfigV1alpha2Interface
	// Deprecated: please explicitly pick a version if possible.
	Config() configv1alpha2.ConfigV1alpha2Interface
}

// Clientset contains the clients for groups. Each group has exactly one
// version included in a Clientset.
type Clientset struct {
	*discovery.DiscoveryClient
	cloudbuildV1alpha1 *cloudbuildv1alpha1.CloudbuildV1alpha1Client
	elafrosV1alpha1    *elafrosv1alpha1.ElafrosV1alpha1Client
	configV1alpha2     *configv1alpha2.ConfigV1alpha2Client
}

// CloudbuildV1alpha1 retrieves the CloudbuildV1alpha1Client
func (c *Clientset) CloudbuildV1alpha1() cloudbuildv1alpha1.CloudbuildV1alpha1Interface {
	return c.cloudbuildV1alpha1
}

// Deprecated: Cloudbuild retrieves the default version of CloudbuildClient.
// Please explicitly pick a version.
func (c *Clientset) Cloudbuild() cloudbuildv1alpha1.CloudbuildV1alpha1Interface {
	return c.cloudbuildV1alpha1
}

// ElafrosV1alpha1 retrieves the ElafrosV1alpha1Client
func (c *Clientset) ElafrosV1alpha1() elafrosv1alpha1.ElafrosV1alpha1Interface {
	return c.elafrosV1alpha1
}

// Deprecated: Elafros retrieves the default version of ElafrosClient.
// Please explicitly pick a version.
func (c *Clientset) Elafros() elafrosv1alpha1.ElafrosV1alpha1Interface {
	return c.elafrosV1alpha1
}

// ConfigV1alpha2 retrieves the ConfigV1alpha2Client
func (c *Clientset) ConfigV1alpha2() configv1alpha2.ConfigV1alpha2Interface {
	return c.configV1alpha2
}

// Deprecated: Config retrieves the default version of ConfigClient.
// Please explicitly pick a version.
func (c *Clientset) Config() configv1alpha2.ConfigV1alpha2Interface {
	return c.configV1alpha2
}

// Discovery retrieves the DiscoveryClient
func (c *Clientset) Discovery() discovery.DiscoveryInterface {
	if c == nil {
		return nil
	}
	return c.DiscoveryClient
}

// NewForConfig creates a new Clientset for the given config.
func NewForConfig(c *rest.Config) (*Clientset, error) {
	configShallowCopy := *c
	if configShallowCopy.RateLimiter == nil && configShallowCopy.QPS > 0 {
		configShallowCopy.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(configShallowCopy.QPS, configShallowCopy.Burst)
	}
	var cs Clientset
	var err error
	cs.cloudbuildV1alpha1, err = cloudbuildv1alpha1.NewForConfig(&configShallowCopy)
	if err != nil {
		return nil, err
	}
	cs.elafrosV1alpha1, err = elafrosv1alpha1.NewForConfig(&configShallowCopy)
	if err != nil {
		return nil, err
	}
	cs.configV1alpha2, err = configv1alpha2.NewForConfig(&configShallowCopy)
	if err != nil {
		return nil, err
	}

	cs.DiscoveryClient, err = discovery.NewDiscoveryClientForConfig(&configShallowCopy)
	if err != nil {
		glog.Errorf("failed to create the DiscoveryClient: %v", err)
		return nil, err
	}
	return &cs, nil
}

// NewForConfigOrDie creates a new Clientset for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *Clientset {
	var cs Clientset
	cs.cloudbuildV1alpha1 = cloudbuildv1alpha1.NewForConfigOrDie(c)
	cs.elafrosV1alpha1 = elafrosv1alpha1.NewForConfigOrDie(c)
	cs.configV1alpha2 = configv1alpha2.NewForConfigOrDie(c)

	cs.DiscoveryClient = discovery.NewDiscoveryClientForConfigOrDie(c)
	return &cs
}

// New creates a new Clientset for the given RESTClient.
func New(c rest.Interface) *Clientset {
	var cs Clientset
	cs.cloudbuildV1alpha1 = cloudbuildv1alpha1.New(c)
	cs.elafrosV1alpha1 = elafrosv1alpha1.New(c)
	cs.configV1alpha2 = configv1alpha2.New(c)

	cs.DiscoveryClient = discovery.NewDiscoveryClient(c)
	return &cs
}
