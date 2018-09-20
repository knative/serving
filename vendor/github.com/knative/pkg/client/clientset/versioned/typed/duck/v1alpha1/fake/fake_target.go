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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeTargets implements TargetInterface
type FakeTargets struct {
	Fake *FakeDuckV1alpha1
	ns   string
}

var targetsResource = schema.GroupVersionResource{Group: "duck.knative.dev", Version: "v1alpha1", Resource: "targets"}

var targetsKind = schema.GroupVersionKind{Group: "duck.knative.dev", Version: "v1alpha1", Kind: "Target"}

// Get takes name of the target, and returns the corresponding target object, and an error if there is any.
func (c *FakeTargets) Get(name string, options v1.GetOptions) (result *v1alpha1.Target, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(targetsResource, c.ns, name), &v1alpha1.Target{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Target), err
}

// List takes label and field selectors, and returns the list of Targets that match those selectors.
func (c *FakeTargets) List(opts v1.ListOptions) (result *v1alpha1.TargetList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(targetsResource, targetsKind, c.ns, opts), &v1alpha1.TargetList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.TargetList{ListMeta: obj.(*v1alpha1.TargetList).ListMeta}
	for _, item := range obj.(*v1alpha1.TargetList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested targets.
func (c *FakeTargets) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(targetsResource, c.ns, opts))

}

// Create takes the representation of a target and creates it.  Returns the server's representation of the target, and an error, if there is any.
func (c *FakeTargets) Create(target *v1alpha1.Target) (result *v1alpha1.Target, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(targetsResource, c.ns, target), &v1alpha1.Target{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Target), err
}

// Update takes the representation of a target and updates it. Returns the server's representation of the target, and an error, if there is any.
func (c *FakeTargets) Update(target *v1alpha1.Target) (result *v1alpha1.Target, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(targetsResource, c.ns, target), &v1alpha1.Target{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Target), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeTargets) UpdateStatus(target *v1alpha1.Target) (*v1alpha1.Target, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(targetsResource, "status", c.ns, target), &v1alpha1.Target{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Target), err
}

// Delete takes name of the target and deletes it. Returns an error if one occurs.
func (c *FakeTargets) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(targetsResource, c.ns, name), &v1alpha1.Target{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeTargets) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(targetsResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v1alpha1.TargetList{})
	return err
}

// Patch applies the patch and returns the patched target.
func (c *FakeTargets) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.Target, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(targetsResource, c.ns, name, data, subresources...), &v1alpha1.Target{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Target), err
}
