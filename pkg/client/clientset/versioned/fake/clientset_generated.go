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
package fake

import (
	clientset "github.com/google/elafros/pkg/client/clientset/versioned"
	cloudbuildv1alpha1 "github.com/google/elafros/pkg/client/clientset/versioned/typed/cloudbuild/v1alpha1"
	fakecloudbuildv1alpha1 "github.com/google/elafros/pkg/client/clientset/versioned/typed/cloudbuild/v1alpha1/fake"
	elafrosv1alpha1 "github.com/google/elafros/pkg/client/clientset/versioned/typed/ela/v1alpha1"
	fakeelafrosv1alpha1 "github.com/google/elafros/pkg/client/clientset/versioned/typed/ela/v1alpha1/fake"
	configv1alpha2 "github.com/google/elafros/pkg/client/clientset/versioned/typed/istio/v1alpha2"
	fakeconfigv1alpha2 "github.com/google/elafros/pkg/client/clientset/versioned/typed/istio/v1alpha2/fake"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/testing"
)

// NewSimpleClientset returns a clientset that will respond with the provided objects.
// It's backed by a very simple object tracker that processes creates, updates and deletions as-is,
// without applying any validations and/or defaults. It shouldn't be considered a replacement
// for a real clientset and is mostly useful in simple unit tests.
func NewSimpleClientset(objects ...runtime.Object) *Clientset {
	o := testing.NewObjectTracker(scheme, codecs.UniversalDecoder())
	for _, obj := range objects {
		if err := o.Add(obj); err != nil {
			panic(err)
		}
	}

	fakePtr := testing.Fake{}
	fakePtr.AddReactor("*", "*", testing.ObjectReaction(o))
	fakePtr.AddWatchReactor("*", testing.DefaultWatchReactor(watch.NewFake(), nil))

	return &Clientset{fakePtr, &fakediscovery.FakeDiscovery{Fake: &fakePtr}}
}

// Clientset implements clientset.Interface. Meant to be embedded into a
// struct to get a default implementation. This makes faking out just the method
// you want to test easier.
type Clientset struct {
	testing.Fake
	discovery *fakediscovery.FakeDiscovery
}

func (c *Clientset) Discovery() discovery.DiscoveryInterface {
	return c.discovery
}

var _ clientset.Interface = &Clientset{}

// CloudbuildV1alpha1 retrieves the CloudbuildV1alpha1Client
func (c *Clientset) CloudbuildV1alpha1() cloudbuildv1alpha1.CloudbuildV1alpha1Interface {
	return &fakecloudbuildv1alpha1.FakeCloudbuildV1alpha1{Fake: &c.Fake}
}

// Cloudbuild retrieves the CloudbuildV1alpha1Client
func (c *Clientset) Cloudbuild() cloudbuildv1alpha1.CloudbuildV1alpha1Interface {
	return &fakecloudbuildv1alpha1.FakeCloudbuildV1alpha1{Fake: &c.Fake}
}

// ElafrosV1alpha1 retrieves the ElafrosV1alpha1Client
func (c *Clientset) ElafrosV1alpha1() elafrosv1alpha1.ElafrosV1alpha1Interface {
	return &fakeelafrosv1alpha1.FakeElafrosV1alpha1{Fake: &c.Fake}
}

// Elafros retrieves the ElafrosV1alpha1Client
func (c *Clientset) Elafros() elafrosv1alpha1.ElafrosV1alpha1Interface {
	return &fakeelafrosv1alpha1.FakeElafrosV1alpha1{Fake: &c.Fake}
}

// ConfigV1alpha2 retrieves the ConfigV1alpha2Client
func (c *Clientset) ConfigV1alpha2() configv1alpha2.ConfigV1alpha2Interface {
	return &fakeconfigv1alpha2.FakeConfigV1alpha2{Fake: &c.Fake}
}

// Config retrieves the ConfigV1alpha2Client
func (c *Clientset) Config() configv1alpha2.ConfigV1alpha2Interface {
	return &fakeconfigv1alpha2.FakeConfigV1alpha2{Fake: &c.Fake}
}
