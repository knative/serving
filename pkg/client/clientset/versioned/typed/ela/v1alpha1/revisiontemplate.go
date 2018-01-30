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
	v1alpha1 "github.com/google/elafros/pkg/apis/ela/v1alpha1"
	scheme "github.com/google/elafros/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// RevisionTemplatesGetter has a method to return a RevisionTemplateInterface.
// A group's client should implement this interface.
type RevisionTemplatesGetter interface {
	RevisionTemplates(namespace string) RevisionTemplateInterface
}

// RevisionTemplateInterface has methods to work with RevisionTemplate resources.
type RevisionTemplateInterface interface {
	Create(*v1alpha1.RevisionTemplate) (*v1alpha1.RevisionTemplate, error)
	Update(*v1alpha1.RevisionTemplate) (*v1alpha1.RevisionTemplate, error)
	UpdateStatus(*v1alpha1.RevisionTemplate) (*v1alpha1.RevisionTemplate, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1alpha1.RevisionTemplate, error)
	List(opts v1.ListOptions) (*v1alpha1.RevisionTemplateList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.RevisionTemplate, err error)
	RevisionTemplateExpansion
}

// revisionTemplates implements RevisionTemplateInterface
type revisionTemplates struct {
	client rest.Interface
	ns     string
}

// newRevisionTemplates returns a RevisionTemplates
func newRevisionTemplates(c *ElafrosV1alpha1Client, namespace string) *revisionTemplates {
	return &revisionTemplates{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the revisionTemplate, and returns the corresponding revisionTemplate object, and an error if there is any.
func (c *revisionTemplates) Get(name string, options v1.GetOptions) (result *v1alpha1.RevisionTemplate, err error) {
	result = &v1alpha1.RevisionTemplate{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("revisiontemplates").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of RevisionTemplates that match those selectors.
func (c *revisionTemplates) List(opts v1.ListOptions) (result *v1alpha1.RevisionTemplateList, err error) {
	result = &v1alpha1.RevisionTemplateList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("revisiontemplates").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested revisionTemplates.
func (c *revisionTemplates) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("revisiontemplates").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a revisionTemplate and creates it.  Returns the server's representation of the revisionTemplate, and an error, if there is any.
func (c *revisionTemplates) Create(revisionTemplate *v1alpha1.RevisionTemplate) (result *v1alpha1.RevisionTemplate, err error) {
	result = &v1alpha1.RevisionTemplate{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("revisiontemplates").
		Body(revisionTemplate).
		Do().
		Into(result)
	return
}

// Update takes the representation of a revisionTemplate and updates it. Returns the server's representation of the revisionTemplate, and an error, if there is any.
func (c *revisionTemplates) Update(revisionTemplate *v1alpha1.RevisionTemplate) (result *v1alpha1.RevisionTemplate, err error) {
	result = &v1alpha1.RevisionTemplate{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("revisiontemplates").
		Name(revisionTemplate.Name).
		Body(revisionTemplate).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *revisionTemplates) UpdateStatus(revisionTemplate *v1alpha1.RevisionTemplate) (result *v1alpha1.RevisionTemplate, err error) {
	result = &v1alpha1.RevisionTemplate{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("revisiontemplates").
		Name(revisionTemplate.Name).
		SubResource("status").
		Body(revisionTemplate).
		Do().
		Into(result)
	return
}

// Delete takes name of the revisionTemplate and deletes it. Returns an error if one occurs.
func (c *revisionTemplates) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("revisiontemplates").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *revisionTemplates) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("revisiontemplates").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched revisionTemplate.
func (c *revisionTemplates) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.RevisionTemplate, err error) {
	result = &v1alpha1.RevisionTemplate{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("revisiontemplates").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
