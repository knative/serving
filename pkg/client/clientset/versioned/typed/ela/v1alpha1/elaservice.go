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

// ElaServicesGetter has a method to return a ElaServiceInterface.
// A group's client should implement this interface.
type ElaServicesGetter interface {
	ElaServices(namespace string) ElaServiceInterface
}

// ElaServiceInterface has methods to work with ElaService resources.
type ElaServiceInterface interface {
	Create(*v1alpha1.ElaService) (*v1alpha1.ElaService, error)
	Update(*v1alpha1.ElaService) (*v1alpha1.ElaService, error)
	UpdateStatus(*v1alpha1.ElaService) (*v1alpha1.ElaService, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1alpha1.ElaService, error)
	List(opts v1.ListOptions) (*v1alpha1.ElaServiceList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.ElaService, err error)
	ElaServiceExpansion
}

// elaServices implements ElaServiceInterface
type elaServices struct {
	client rest.Interface
	ns     string
}

// newElaServices returns a ElaServices
func newElaServices(c *ElafrosV1alpha1Client, namespace string) *elaServices {
	return &elaServices{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the elaService, and returns the corresponding elaService object, and an error if there is any.
func (c *elaServices) Get(name string, options v1.GetOptions) (result *v1alpha1.ElaService, err error) {
	result = &v1alpha1.ElaService{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("elaservices").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of ElaServices that match those selectors.
func (c *elaServices) List(opts v1.ListOptions) (result *v1alpha1.ElaServiceList, err error) {
	result = &v1alpha1.ElaServiceList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("elaservices").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested elaServices.
func (c *elaServices) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("elaservices").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a elaService and creates it.  Returns the server's representation of the elaService, and an error, if there is any.
func (c *elaServices) Create(elaService *v1alpha1.ElaService) (result *v1alpha1.ElaService, err error) {
	result = &v1alpha1.ElaService{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("elaservices").
		Body(elaService).
		Do().
		Into(result)
	return
}

// Update takes the representation of a elaService and updates it. Returns the server's representation of the elaService, and an error, if there is any.
func (c *elaServices) Update(elaService *v1alpha1.ElaService) (result *v1alpha1.ElaService, err error) {
	result = &v1alpha1.ElaService{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("elaservices").
		Name(elaService.Name).
		Body(elaService).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *elaServices) UpdateStatus(elaService *v1alpha1.ElaService) (result *v1alpha1.ElaService, err error) {
	result = &v1alpha1.ElaService{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("elaservices").
		Name(elaService.Name).
		SubResource("status").
		Body(elaService).
		Do().
		Into(result)
	return
}

// Delete takes name of the elaService and deletes it. Returns an error if one occurs.
func (c *elaServices) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("elaservices").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *elaServices) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("elaservices").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched elaService.
func (c *elaServices) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.ElaService, err error) {
	result = &v1alpha1.ElaService{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("elaservices").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
