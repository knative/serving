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

// FakeVerticalPodAutoscalerCheckpoints implements VerticalPodAutoscalerCheckpointInterface
type FakeVerticalPodAutoscalerCheckpoints struct {
	Fake *FakePocV1alpha1
	ns   string
}

var verticalpodautoscalercheckpointsResource = schema.GroupVersionResource{Group: "poc.autoscaling.k8s.io", Version: "v1alpha1", Resource: "verticalpodautoscalercheckpoints"}

var verticalpodautoscalercheckpointsKind = schema.GroupVersionKind{Group: "poc.autoscaling.k8s.io", Version: "v1alpha1", Kind: "VerticalPodAutoscalerCheckpoint"}

// Get takes name of the verticalPodAutoscalerCheckpoint, and returns the corresponding verticalPodAutoscalerCheckpoint object, and an error if there is any.
func (c *FakeVerticalPodAutoscalerCheckpoints) Get(name string, options v1.GetOptions) (result *v1alpha1.VerticalPodAutoscalerCheckpoint, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(verticalpodautoscalercheckpointsResource, c.ns, name), &v1alpha1.VerticalPodAutoscalerCheckpoint{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.VerticalPodAutoscalerCheckpoint), err
}

// List takes label and field selectors, and returns the list of VerticalPodAutoscalerCheckpoints that match those selectors.
func (c *FakeVerticalPodAutoscalerCheckpoints) List(opts v1.ListOptions) (result *v1alpha1.VerticalPodAutoscalerCheckpointList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(verticalpodautoscalercheckpointsResource, verticalpodautoscalercheckpointsKind, c.ns, opts), &v1alpha1.VerticalPodAutoscalerCheckpointList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.VerticalPodAutoscalerCheckpointList{}
	for _, item := range obj.(*v1alpha1.VerticalPodAutoscalerCheckpointList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested verticalPodAutoscalerCheckpoints.
func (c *FakeVerticalPodAutoscalerCheckpoints) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(verticalpodautoscalercheckpointsResource, c.ns, opts))

}

// Create takes the representation of a verticalPodAutoscalerCheckpoint and creates it.  Returns the server's representation of the verticalPodAutoscalerCheckpoint, and an error, if there is any.
func (c *FakeVerticalPodAutoscalerCheckpoints) Create(verticalPodAutoscalerCheckpoint *v1alpha1.VerticalPodAutoscalerCheckpoint) (result *v1alpha1.VerticalPodAutoscalerCheckpoint, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(verticalpodautoscalercheckpointsResource, c.ns, verticalPodAutoscalerCheckpoint), &v1alpha1.VerticalPodAutoscalerCheckpoint{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.VerticalPodAutoscalerCheckpoint), err
}

// Update takes the representation of a verticalPodAutoscalerCheckpoint and updates it. Returns the server's representation of the verticalPodAutoscalerCheckpoint, and an error, if there is any.
func (c *FakeVerticalPodAutoscalerCheckpoints) Update(verticalPodAutoscalerCheckpoint *v1alpha1.VerticalPodAutoscalerCheckpoint) (result *v1alpha1.VerticalPodAutoscalerCheckpoint, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(verticalpodautoscalercheckpointsResource, c.ns, verticalPodAutoscalerCheckpoint), &v1alpha1.VerticalPodAutoscalerCheckpoint{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.VerticalPodAutoscalerCheckpoint), err
}

// Delete takes name of the verticalPodAutoscalerCheckpoint and deletes it. Returns an error if one occurs.
func (c *FakeVerticalPodAutoscalerCheckpoints) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(verticalpodautoscalercheckpointsResource, c.ns, name), &v1alpha1.VerticalPodAutoscalerCheckpoint{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeVerticalPodAutoscalerCheckpoints) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(verticalpodautoscalercheckpointsResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v1alpha1.VerticalPodAutoscalerCheckpointList{})
	return err
}

// Patch applies the patch and returns the patched verticalPodAutoscalerCheckpoint.
func (c *FakeVerticalPodAutoscalerCheckpoints) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.VerticalPodAutoscalerCheckpoint, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(verticalpodautoscalercheckpointsResource, c.ns, name, data, subresources...), &v1alpha1.VerticalPodAutoscalerCheckpoint{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.VerticalPodAutoscalerCheckpoint), err
}
