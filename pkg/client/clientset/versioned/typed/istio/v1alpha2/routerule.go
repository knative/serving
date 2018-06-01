/*
Copyright 2018 Google LLC

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
package v1alpha2

import (
	v1alpha2 "github.com/knative/serving/pkg/apis/istio/v1alpha2"
	scheme "github.com/knative/serving/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// RouteRulesGetter has a method to return a RouteRuleInterface.
// A group's client should implement this interface.
type RouteRulesGetter interface {
	RouteRules(namespace string) RouteRuleInterface
}

// RouteRuleInterface has methods to work with RouteRule resources.
type RouteRuleInterface interface {
	Create(*v1alpha2.RouteRule) (*v1alpha2.RouteRule, error)
	Update(*v1alpha2.RouteRule) (*v1alpha2.RouteRule, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1alpha2.RouteRule, error)
	List(opts v1.ListOptions) (*v1alpha2.RouteRuleList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha2.RouteRule, err error)
	RouteRuleExpansion
}

// routeRules implements RouteRuleInterface
type routeRules struct {
	client rest.Interface
	ns     string
}

// newRouteRules returns a RouteRules
func newRouteRules(c *ConfigV1alpha2Client, namespace string) *routeRules {
	return &routeRules{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the routeRule, and returns the corresponding routeRule object, and an error if there is any.
func (c *routeRules) Get(name string, options v1.GetOptions) (result *v1alpha2.RouteRule, err error) {
	result = &v1alpha2.RouteRule{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("routerules").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of RouteRules that match those selectors.
func (c *routeRules) List(opts v1.ListOptions) (result *v1alpha2.RouteRuleList, err error) {
	result = &v1alpha2.RouteRuleList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("routerules").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested routeRules.
func (c *routeRules) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("routerules").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a routeRule and creates it.  Returns the server's representation of the routeRule, and an error, if there is any.
func (c *routeRules) Create(routeRule *v1alpha2.RouteRule) (result *v1alpha2.RouteRule, err error) {
	result = &v1alpha2.RouteRule{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("routerules").
		Body(routeRule).
		Do().
		Into(result)
	return
}

// Update takes the representation of a routeRule and updates it. Returns the server's representation of the routeRule, and an error, if there is any.
func (c *routeRules) Update(routeRule *v1alpha2.RouteRule) (result *v1alpha2.RouteRule, err error) {
	result = &v1alpha2.RouteRule{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("routerules").
		Name(routeRule.Name).
		Body(routeRule).
		Do().
		Into(result)
	return
}

// Delete takes name of the routeRule and deletes it. Returns an error if one occurs.
func (c *routeRules) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("routerules").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *routeRules) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("routerules").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched routeRule.
func (c *routeRules) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha2.RouteRule, err error) {
	result = &v1alpha2.RouteRule{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("routerules").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
