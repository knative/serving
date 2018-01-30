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
	v1alpha2 "github.com/google/elafros/pkg/apis/istio/v1alpha2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeRouteRules implements RouteRuleInterface
type FakeRouteRules struct {
	Fake *FakeConfigV1alpha2
	ns   string
}

var routerulesResource = schema.GroupVersionResource{Group: "config.istio.io", Version: "v1alpha2", Resource: "routerules"}

var routerulesKind = schema.GroupVersionKind{Group: "config.istio.io", Version: "v1alpha2", Kind: "RouteRule"}

// Get takes name of the routeRule, and returns the corresponding routeRule object, and an error if there is any.
func (c *FakeRouteRules) Get(name string, options v1.GetOptions) (result *v1alpha2.RouteRule, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(routerulesResource, c.ns, name), &v1alpha2.RouteRule{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha2.RouteRule), err
}

// List takes label and field selectors, and returns the list of RouteRules that match those selectors.
func (c *FakeRouteRules) List(opts v1.ListOptions) (result *v1alpha2.RouteRuleList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(routerulesResource, routerulesKind, c.ns, opts), &v1alpha2.RouteRuleList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha2.RouteRuleList{}
	for _, item := range obj.(*v1alpha2.RouteRuleList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested routeRules.
func (c *FakeRouteRules) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(routerulesResource, c.ns, opts))

}

// Create takes the representation of a routeRule and creates it.  Returns the server's representation of the routeRule, and an error, if there is any.
func (c *FakeRouteRules) Create(routeRule *v1alpha2.RouteRule) (result *v1alpha2.RouteRule, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(routerulesResource, c.ns, routeRule), &v1alpha2.RouteRule{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha2.RouteRule), err
}

// Update takes the representation of a routeRule and updates it. Returns the server's representation of the routeRule, and an error, if there is any.
func (c *FakeRouteRules) Update(routeRule *v1alpha2.RouteRule) (result *v1alpha2.RouteRule, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(routerulesResource, c.ns, routeRule), &v1alpha2.RouteRule{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha2.RouteRule), err
}

// Delete takes name of the routeRule and deletes it. Returns an error if one occurs.
func (c *FakeRouteRules) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(routerulesResource, c.ns, name), &v1alpha2.RouteRule{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeRouteRules) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(routerulesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v1alpha2.RouteRuleList{})
	return err
}

// Patch applies the patch and returns the patched routeRule.
func (c *FakeRouteRules) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha2.RouteRule, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(routerulesResource, c.ns, name, data, subresources...), &v1alpha2.RouteRule{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha2.RouteRule), err
}
