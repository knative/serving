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
	v1alpha1 "github.com/google/elafros/pkg/apis/ela/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeRevisionTemplates implements RevisionTemplateInterface
type FakeRevisionTemplates struct {
	Fake *FakeElafrosV1alpha1
	ns   string
}

var revisiontemplatesResource = schema.GroupVersionResource{Group: "elafros.dev", Version: "v1alpha1", Resource: "revisiontemplates"}

var revisiontemplatesKind = schema.GroupVersionKind{Group: "elafros.dev", Version: "v1alpha1", Kind: "RevisionTemplate"}

// Get takes name of the revisionTemplate, and returns the corresponding revisionTemplate object, and an error if there is any.
func (c *FakeRevisionTemplates) Get(name string, options v1.GetOptions) (result *v1alpha1.RevisionTemplate, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(revisiontemplatesResource, c.ns, name), &v1alpha1.RevisionTemplate{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.RevisionTemplate), err
}

// List takes label and field selectors, and returns the list of RevisionTemplates that match those selectors.
func (c *FakeRevisionTemplates) List(opts v1.ListOptions) (result *v1alpha1.RevisionTemplateList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(revisiontemplatesResource, revisiontemplatesKind, c.ns, opts), &v1alpha1.RevisionTemplateList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.RevisionTemplateList{}
	for _, item := range obj.(*v1alpha1.RevisionTemplateList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested revisionTemplates.
func (c *FakeRevisionTemplates) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(revisiontemplatesResource, c.ns, opts))

}

// Create takes the representation of a revisionTemplate and creates it.  Returns the server's representation of the revisionTemplate, and an error, if there is any.
func (c *FakeRevisionTemplates) Create(revisionTemplate *v1alpha1.RevisionTemplate) (result *v1alpha1.RevisionTemplate, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(revisiontemplatesResource, c.ns, revisionTemplate), &v1alpha1.RevisionTemplate{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.RevisionTemplate), err
}

// Update takes the representation of a revisionTemplate and updates it. Returns the server's representation of the revisionTemplate, and an error, if there is any.
func (c *FakeRevisionTemplates) Update(revisionTemplate *v1alpha1.RevisionTemplate) (result *v1alpha1.RevisionTemplate, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(revisiontemplatesResource, c.ns, revisionTemplate), &v1alpha1.RevisionTemplate{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.RevisionTemplate), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeRevisionTemplates) UpdateStatus(revisionTemplate *v1alpha1.RevisionTemplate) (*v1alpha1.RevisionTemplate, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(revisiontemplatesResource, "status", c.ns, revisionTemplate), &v1alpha1.RevisionTemplate{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.RevisionTemplate), err
}

// Delete takes name of the revisionTemplate and deletes it. Returns an error if one occurs.
func (c *FakeRevisionTemplates) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(revisiontemplatesResource, c.ns, name), &v1alpha1.RevisionTemplate{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeRevisionTemplates) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(revisiontemplatesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v1alpha1.RevisionTemplateList{})
	return err
}

// Patch applies the patch and returns the patched revisionTemplate.
func (c *FakeRevisionTemplates) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.RevisionTemplate, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(revisiontemplatesResource, c.ns, name, data, subresources...), &v1alpha1.RevisionTemplate{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.RevisionTemplate), err
}
