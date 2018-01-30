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

// FakeElaServices implements ElaServiceInterface
type FakeElaServices struct {
	Fake *FakeElafrosV1alpha1
	ns   string
}

var elaservicesResource = schema.GroupVersionResource{Group: "elafros.dev", Version: "v1alpha1", Resource: "elaservices"}

var elaservicesKind = schema.GroupVersionKind{Group: "elafros.dev", Version: "v1alpha1", Kind: "ElaService"}

// Get takes name of the elaService, and returns the corresponding elaService object, and an error if there is any.
func (c *FakeElaServices) Get(name string, options v1.GetOptions) (result *v1alpha1.ElaService, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(elaservicesResource, c.ns, name), &v1alpha1.ElaService{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ElaService), err
}

// List takes label and field selectors, and returns the list of ElaServices that match those selectors.
func (c *FakeElaServices) List(opts v1.ListOptions) (result *v1alpha1.ElaServiceList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(elaservicesResource, elaservicesKind, c.ns, opts), &v1alpha1.ElaServiceList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.ElaServiceList{}
	for _, item := range obj.(*v1alpha1.ElaServiceList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested elaServices.
func (c *FakeElaServices) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(elaservicesResource, c.ns, opts))

}

// Create takes the representation of a elaService and creates it.  Returns the server's representation of the elaService, and an error, if there is any.
func (c *FakeElaServices) Create(elaService *v1alpha1.ElaService) (result *v1alpha1.ElaService, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(elaservicesResource, c.ns, elaService), &v1alpha1.ElaService{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ElaService), err
}

// Update takes the representation of a elaService and updates it. Returns the server's representation of the elaService, and an error, if there is any.
func (c *FakeElaServices) Update(elaService *v1alpha1.ElaService) (result *v1alpha1.ElaService, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(elaservicesResource, c.ns, elaService), &v1alpha1.ElaService{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ElaService), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeElaServices) UpdateStatus(elaService *v1alpha1.ElaService) (*v1alpha1.ElaService, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(elaservicesResource, "status", c.ns, elaService), &v1alpha1.ElaService{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ElaService), err
}

// Delete takes name of the elaService and deletes it. Returns an error if one occurs.
func (c *FakeElaServices) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(elaservicesResource, c.ns, name), &v1alpha1.ElaService{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeElaServices) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(elaservicesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v1alpha1.ElaServiceList{})
	return err
}

// Patch applies the patch and returns the patched elaService.
func (c *FakeElaServices) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.ElaService, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(elaservicesResource, c.ns, name, data, subresources...), &v1alpha1.ElaService{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ElaService), err
}
