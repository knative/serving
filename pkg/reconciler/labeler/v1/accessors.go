/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/kmeta"

	"knative.dev/serving/pkg/apis/serving"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
)

// Accessor defines an abstraction for manipulating labeled entity
// (Configuration, Revision) with shared logic.
type Accessor interface {
	get(ns, name string) (kmeta.Accessor, error)
	list(ns, name string) ([]kmeta.Accessor, error)
	patch(ns, name string, pt types.PatchType, p []byte) error
}

// Revision is an implementation of Accessor for Revisions.
type Revision struct {
	client         clientset.Interface
	revisionLister listers.RevisionLister
}

// revision implements Accessor
var _ Accessor = (*Revision)(nil)

// NewRevisionAccessor is a factory function to make a new revision accessor.
func NewRevisionAccessor(client clientset.Interface, lister listers.RevisionLister) *Revision {
	return &Revision{
		client:         client,
		revisionLister: lister,
	}
}

// get implements Accessor
func (r *Revision) get(ns, name string) (kmeta.Accessor, error) {
	return r.revisionLister.Revisions(ns).Get(name)
}

// list implements Accessor
func (r *Revision) list(ns, name string) ([]kmeta.Accessor, error) {
	rl, err := r.revisionLister.Revisions(ns).List(labels.SelectorFromSet(labels.Set{
		serving.RouteLabelKey: name,
	}))
	if err != nil {
		return nil, err
	}
	// Need a copy to change types in Go
	kl := make([]kmeta.Accessor, 0, len(rl))
	for _, r := range rl {
		kl = append(kl, r)
	}
	return kl, err
}

// patch implements Accessor
func (r *Revision) patch(ns, name string, pt types.PatchType, p []byte) error {
	_, err := r.client.ServingV1().Revisions(ns).Patch(name, pt, p)
	return err
}

// Configuration is an implementation of Accessor for Configurations.
type Configuration struct {
	client              clientset.Interface
	configurationLister listers.ConfigurationLister
}

// configuration implements Accessor
var _ Accessor = (*Configuration)(nil)

// NewConfigurationAccessor is a factory function to make a new configuration Accessor.
func NewConfigurationAccessor(client clientset.Interface, lister listers.ConfigurationLister) *Configuration {
	return &Configuration{
		client:              client,
		configurationLister: lister,
	}
}

// get implements Accessor
func (c *Configuration) get(ns, name string) (kmeta.Accessor, error) {
	return c.configurationLister.Configurations(ns).Get(name)
}

// list implements Accessor
func (c *Configuration) list(ns, name string) ([]kmeta.Accessor, error) {
	rl, err := c.configurationLister.Configurations(ns).List(labels.SelectorFromSet(labels.Set{
		serving.RouteLabelKey: name,
	}))
	if err != nil {
		return nil, err
	}
	// Need a copy to change types in Go
	kl := make([]kmeta.Accessor, 0, len(rl))
	for _, r := range rl {
		kl = append(kl, r)
	}
	return kl, err
}

// patch implements Accessor
func (c *Configuration) patch(ns, name string, pt types.PatchType, p []byte) error {
	_, err := c.client.ServingV1().Configurations(ns).Patch(name, pt, p)
	return err
}
