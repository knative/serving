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

package v1alpha1

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	v1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/poc.autoscaling.k8s.io/v1alpha1"
	scheme "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned/scheme"
	rest "k8s.io/client-go/rest"
)

// VerticalPodAutoscalerCheckpointsGetter has a method to return a VerticalPodAutoscalerCheckpointInterface.
// A group's client should implement this interface.
type VerticalPodAutoscalerCheckpointsGetter interface {
	VerticalPodAutoscalerCheckpoints(namespace string) VerticalPodAutoscalerCheckpointInterface
}

// VerticalPodAutoscalerCheckpointInterface has methods to work with VerticalPodAutoscalerCheckpoint resources.
type VerticalPodAutoscalerCheckpointInterface interface {
	Create(*v1alpha1.VerticalPodAutoscalerCheckpoint) (*v1alpha1.VerticalPodAutoscalerCheckpoint, error)
	Update(*v1alpha1.VerticalPodAutoscalerCheckpoint) (*v1alpha1.VerticalPodAutoscalerCheckpoint, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1alpha1.VerticalPodAutoscalerCheckpoint, error)
	List(opts v1.ListOptions) (*v1alpha1.VerticalPodAutoscalerCheckpointList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.VerticalPodAutoscalerCheckpoint, err error)
	VerticalPodAutoscalerCheckpointExpansion
}

// verticalPodAutoscalerCheckpoints implements VerticalPodAutoscalerCheckpointInterface
type verticalPodAutoscalerCheckpoints struct {
	client rest.Interface
	ns     string
}

// newVerticalPodAutoscalerCheckpoints returns a VerticalPodAutoscalerCheckpoints
func newVerticalPodAutoscalerCheckpoints(c *PocV1alpha1Client, namespace string) *verticalPodAutoscalerCheckpoints {
	return &verticalPodAutoscalerCheckpoints{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the verticalPodAutoscalerCheckpoint, and returns the corresponding verticalPodAutoscalerCheckpoint object, and an error if there is any.
func (c *verticalPodAutoscalerCheckpoints) Get(name string, options v1.GetOptions) (result *v1alpha1.VerticalPodAutoscalerCheckpoint, err error) {
	result = &v1alpha1.VerticalPodAutoscalerCheckpoint{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("verticalpodautoscalercheckpoints").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of VerticalPodAutoscalerCheckpoints that match those selectors.
func (c *verticalPodAutoscalerCheckpoints) List(opts v1.ListOptions) (result *v1alpha1.VerticalPodAutoscalerCheckpointList, err error) {
	result = &v1alpha1.VerticalPodAutoscalerCheckpointList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("verticalpodautoscalercheckpoints").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested verticalPodAutoscalerCheckpoints.
func (c *verticalPodAutoscalerCheckpoints) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("verticalpodautoscalercheckpoints").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a verticalPodAutoscalerCheckpoint and creates it.  Returns the server's representation of the verticalPodAutoscalerCheckpoint, and an error, if there is any.
func (c *verticalPodAutoscalerCheckpoints) Create(verticalPodAutoscalerCheckpoint *v1alpha1.VerticalPodAutoscalerCheckpoint) (result *v1alpha1.VerticalPodAutoscalerCheckpoint, err error) {
	result = &v1alpha1.VerticalPodAutoscalerCheckpoint{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("verticalpodautoscalercheckpoints").
		Body(verticalPodAutoscalerCheckpoint).
		Do().
		Into(result)
	return
}

// Update takes the representation of a verticalPodAutoscalerCheckpoint and updates it. Returns the server's representation of the verticalPodAutoscalerCheckpoint, and an error, if there is any.
func (c *verticalPodAutoscalerCheckpoints) Update(verticalPodAutoscalerCheckpoint *v1alpha1.VerticalPodAutoscalerCheckpoint) (result *v1alpha1.VerticalPodAutoscalerCheckpoint, err error) {
	result = &v1alpha1.VerticalPodAutoscalerCheckpoint{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("verticalpodautoscalercheckpoints").
		Name(verticalPodAutoscalerCheckpoint.Name).
		Body(verticalPodAutoscalerCheckpoint).
		Do().
		Into(result)
	return
}

// Delete takes name of the verticalPodAutoscalerCheckpoint and deletes it. Returns an error if one occurs.
func (c *verticalPodAutoscalerCheckpoints) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("verticalpodautoscalercheckpoints").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *verticalPodAutoscalerCheckpoints) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("verticalpodautoscalercheckpoints").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched verticalPodAutoscalerCheckpoint.
func (c *verticalPodAutoscalerCheckpoints) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.VerticalPodAutoscalerCheckpoint, err error) {
	result = &v1alpha1.VerticalPodAutoscalerCheckpoint{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("verticalpodautoscalercheckpoints").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
