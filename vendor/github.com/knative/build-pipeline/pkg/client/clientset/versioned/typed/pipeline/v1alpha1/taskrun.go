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
package v1alpha1

import (
	v1alpha1 "github.com/knative/build-pipeline/pkg/apis/pipeline/v1alpha1"
	scheme "github.com/knative/build-pipeline/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// TaskRunsGetter has a method to return a TaskRunInterface.
// A group's client should implement this interface.
type TaskRunsGetter interface {
	TaskRuns(namespace string) TaskRunInterface
}

// TaskRunInterface has methods to work with TaskRun resources.
type TaskRunInterface interface {
	Create(*v1alpha1.TaskRun) (*v1alpha1.TaskRun, error)
	Update(*v1alpha1.TaskRun) (*v1alpha1.TaskRun, error)
	UpdateStatus(*v1alpha1.TaskRun) (*v1alpha1.TaskRun, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1alpha1.TaskRun, error)
	List(opts v1.ListOptions) (*v1alpha1.TaskRunList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.TaskRun, err error)
	TaskRunExpansion
}

// taskRuns implements TaskRunInterface
type taskRuns struct {
	client rest.Interface
	ns     string
}

// newTaskRuns returns a TaskRuns
func newTaskRuns(c *PipelineV1alpha1Client, namespace string) *taskRuns {
	return &taskRuns{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the taskRun, and returns the corresponding taskRun object, and an error if there is any.
func (c *taskRuns) Get(name string, options v1.GetOptions) (result *v1alpha1.TaskRun, err error) {
	result = &v1alpha1.TaskRun{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("taskruns").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of TaskRuns that match those selectors.
func (c *taskRuns) List(opts v1.ListOptions) (result *v1alpha1.TaskRunList, err error) {
	result = &v1alpha1.TaskRunList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("taskruns").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested taskRuns.
func (c *taskRuns) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("taskruns").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a taskRun and creates it.  Returns the server's representation of the taskRun, and an error, if there is any.
func (c *taskRuns) Create(taskRun *v1alpha1.TaskRun) (result *v1alpha1.TaskRun, err error) {
	result = &v1alpha1.TaskRun{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("taskruns").
		Body(taskRun).
		Do().
		Into(result)
	return
}

// Update takes the representation of a taskRun and updates it. Returns the server's representation of the taskRun, and an error, if there is any.
func (c *taskRuns) Update(taskRun *v1alpha1.TaskRun) (result *v1alpha1.TaskRun, err error) {
	result = &v1alpha1.TaskRun{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("taskruns").
		Name(taskRun.Name).
		Body(taskRun).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *taskRuns) UpdateStatus(taskRun *v1alpha1.TaskRun) (result *v1alpha1.TaskRun, err error) {
	result = &v1alpha1.TaskRun{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("taskruns").
		Name(taskRun.Name).
		SubResource("status").
		Body(taskRun).
		Do().
		Into(result)
	return
}

// Delete takes name of the taskRun and deletes it. Returns an error if one occurs.
func (c *taskRuns) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("taskruns").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *taskRuns) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("taskruns").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched taskRun.
func (c *taskRuns) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.TaskRun, err error) {
	result = &v1alpha1.TaskRun{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("taskruns").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
