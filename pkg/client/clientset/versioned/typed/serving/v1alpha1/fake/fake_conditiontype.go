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
package fake

import (
	v1alpha1 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeConditionTypes implements ConditionTypeInterface
type FakeConditionTypes struct {
	Fake *FakeServingV1alpha1
	ns   string
}

var conditiontypesResource = schema.GroupVersionResource{Group: "serving.knative.dev", Version: "v1alpha1", Resource: "conditiontypes"}

var conditiontypesKind = schema.GroupVersionKind{Group: "serving.knative.dev", Version: "v1alpha1", Kind: "ConditionType"}

// Get takes name of the conditionType, and returns the corresponding conditionType object, and an error if there is any.
func (c *FakeConditionTypes) Get(name string, options v1.GetOptions) (result *v1alpha1.ConditionType, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(conditiontypesResource, c.ns, name), &v1alpha1.ConditionType{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ConditionType), err
}

// List takes label and field selectors, and returns the list of ConditionTypes that match those selectors.
func (c *FakeConditionTypes) List(opts v1.ListOptions) (result *v1alpha1.ConditionTypeList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(conditiontypesResource, conditiontypesKind, c.ns, opts), &v1alpha1.ConditionTypeList{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ConditionTypeList), err
}

// Watch returns a watch.Interface that watches the requested conditionTypes.
func (c *FakeConditionTypes) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(conditiontypesResource, c.ns, opts))

}

// Create takes the representation of a conditionType and creates it.  Returns the server's representation of the conditionType, and an error, if there is any.
func (c *FakeConditionTypes) Create(conditionType *v1alpha1.ConditionType) (result *v1alpha1.ConditionType, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(conditiontypesResource, c.ns, conditionType), &v1alpha1.ConditionType{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ConditionType), err
}

// Update takes the representation of a conditionType and updates it. Returns the server's representation of the conditionType, and an error, if there is any.
func (c *FakeConditionTypes) Update(conditionType *v1alpha1.ConditionType) (result *v1alpha1.ConditionType, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(conditiontypesResource, c.ns, conditionType), &v1alpha1.ConditionType{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ConditionType), err
}

// Delete takes name of the conditionType and deletes it. Returns an error if one occurs.
func (c *FakeConditionTypes) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(conditiontypesResource, c.ns, name), &v1alpha1.ConditionType{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeConditionTypes) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(conditiontypesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v1alpha1.ConditionTypeList{})
	return err
}

// Patch applies the patch and returns the patched conditionType.
func (c *FakeConditionTypes) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.ConditionType, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(conditiontypesResource, c.ns, name, data, subresources...), &v1alpha1.ConditionType{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ConditionType), err
}
