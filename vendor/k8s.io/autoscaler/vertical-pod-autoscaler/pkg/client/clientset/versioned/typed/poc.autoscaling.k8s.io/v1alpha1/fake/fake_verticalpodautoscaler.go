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
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	v1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/poc.autoscaling.k8s.io/v1alpha1"
	testing "k8s.io/client-go/testing"
)

// FakeVerticalPodAutoscalers implements VerticalPodAutoscalerInterface
type FakeVerticalPodAutoscalers struct {
	Fake *FakePocV1alpha1
	ns   string
}

var verticalpodautoscalersResource = schema.GroupVersionResource{Group: "poc.autoscaling.k8s.io", Version: "v1alpha1", Resource: "verticalpodautoscalers"}

var verticalpodautoscalersKind = schema.GroupVersionKind{Group: "poc.autoscaling.k8s.io", Version: "v1alpha1", Kind: "VerticalPodAutoscaler"}

// Get takes name of the verticalPodAutoscaler, and returns the corresponding verticalPodAutoscaler object, and an error if there is any.
func (c *FakeVerticalPodAutoscalers) Get(name string, options v1.GetOptions) (result *v1alpha1.VerticalPodAutoscaler, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(verticalpodautoscalersResource, c.ns, name), &v1alpha1.VerticalPodAutoscaler{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.VerticalPodAutoscaler), err
}

// List takes label and field selectors, and returns the list of VerticalPodAutoscalers that match those selectors.
func (c *FakeVerticalPodAutoscalers) List(opts v1.ListOptions) (result *v1alpha1.VerticalPodAutoscalerList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(verticalpodautoscalersResource, verticalpodautoscalersKind, c.ns, opts), &v1alpha1.VerticalPodAutoscalerList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.VerticalPodAutoscalerList{}
	for _, item := range obj.(*v1alpha1.VerticalPodAutoscalerList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested verticalPodAutoscalers.
func (c *FakeVerticalPodAutoscalers) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(verticalpodautoscalersResource, c.ns, opts))

}

// Create takes the representation of a verticalPodAutoscaler and creates it.  Returns the server's representation of the verticalPodAutoscaler, and an error, if there is any.
func (c *FakeVerticalPodAutoscalers) Create(verticalPodAutoscaler *v1alpha1.VerticalPodAutoscaler) (result *v1alpha1.VerticalPodAutoscaler, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(verticalpodautoscalersResource, c.ns, verticalPodAutoscaler), &v1alpha1.VerticalPodAutoscaler{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.VerticalPodAutoscaler), err
}

// Update takes the representation of a verticalPodAutoscaler and updates it. Returns the server's representation of the verticalPodAutoscaler, and an error, if there is any.
func (c *FakeVerticalPodAutoscalers) Update(verticalPodAutoscaler *v1alpha1.VerticalPodAutoscaler) (result *v1alpha1.VerticalPodAutoscaler, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(verticalpodautoscalersResource, c.ns, verticalPodAutoscaler), &v1alpha1.VerticalPodAutoscaler{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.VerticalPodAutoscaler), err
}

// Delete takes name of the verticalPodAutoscaler and deletes it. Returns an error if one occurs.
func (c *FakeVerticalPodAutoscalers) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(verticalpodautoscalersResource, c.ns, name), &v1alpha1.VerticalPodAutoscaler{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeVerticalPodAutoscalers) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(verticalpodautoscalersResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v1alpha1.VerticalPodAutoscalerList{})
	return err
}

// Patch applies the patch and returns the patched verticalPodAutoscaler.
func (c *FakeVerticalPodAutoscalers) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.VerticalPodAutoscaler, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(verticalpodautoscalersResource, c.ns, name, data, subresources...), &v1alpha1.VerticalPodAutoscaler{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.VerticalPodAutoscaler), err
}
