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
	v1alpha3 "github.com/knative/serving/pkg/apis/istio/v1alpha3"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeDestinationRules implements DestinationRuleInterface
type FakeDestinationRules struct {
	Fake *FakeNetworkingV1alpha3
	ns   string
}

var destinationrulesResource = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "destinationrules"}

var destinationrulesKind = schema.GroupVersionKind{Group: "networking.istio.io", Version: "v1alpha3", Kind: "DestinationRule"}

// Get takes name of the destinationRule, and returns the corresponding destinationRule object, and an error if there is any.
func (c *FakeDestinationRules) Get(name string, options v1.GetOptions) (result *v1alpha3.DestinationRule, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(destinationrulesResource, c.ns, name), &v1alpha3.DestinationRule{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha3.DestinationRule), err
}

// List takes label and field selectors, and returns the list of DestinationRules that match those selectors.
func (c *FakeDestinationRules) List(opts v1.ListOptions) (result *v1alpha3.DestinationRuleList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(destinationrulesResource, destinationrulesKind, c.ns, opts), &v1alpha3.DestinationRuleList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha3.DestinationRuleList{}
	for _, item := range obj.(*v1alpha3.DestinationRuleList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested destinationRules.
func (c *FakeDestinationRules) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(destinationrulesResource, c.ns, opts))

}

// Create takes the representation of a destinationRule and creates it.  Returns the server's representation of the destinationRule, and an error, if there is any.
func (c *FakeDestinationRules) Create(destinationRule *v1alpha3.DestinationRule) (result *v1alpha3.DestinationRule, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(destinationrulesResource, c.ns, destinationRule), &v1alpha3.DestinationRule{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha3.DestinationRule), err
}

// Update takes the representation of a destinationRule and updates it. Returns the server's representation of the destinationRule, and an error, if there is any.
func (c *FakeDestinationRules) Update(destinationRule *v1alpha3.DestinationRule) (result *v1alpha3.DestinationRule, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(destinationrulesResource, c.ns, destinationRule), &v1alpha3.DestinationRule{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha3.DestinationRule), err
}

// Delete takes name of the destinationRule and deletes it. Returns an error if one occurs.
func (c *FakeDestinationRules) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(destinationrulesResource, c.ns, name), &v1alpha3.DestinationRule{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeDestinationRules) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(destinationrulesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v1alpha3.DestinationRuleList{})
	return err
}

// Patch applies the patch and returns the patched destinationRule.
func (c *FakeDestinationRules) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha3.DestinationRule, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(destinationrulesResource, c.ns, name, data, subresources...), &v1alpha3.DestinationRule{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha3.DestinationRule), err
}
