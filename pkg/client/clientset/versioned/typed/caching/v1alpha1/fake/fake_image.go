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
	v1alpha1 "github.com/knative/serving/pkg/apis/caching/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeImages implements ImageInterface
type FakeImages struct {
	Fake *FakeCachingV1alpha1
	ns   string
}

var imagesResource = schema.GroupVersionResource{Group: "caching.internal.knative.dev", Version: "v1alpha1", Resource: "images"}

var imagesKind = schema.GroupVersionKind{Group: "caching.internal.knative.dev", Version: "v1alpha1", Kind: "Image"}

// Get takes name of the image, and returns the corresponding image object, and an error if there is any.
func (c *FakeImages) Get(name string, options v1.GetOptions) (result *v1alpha1.Image, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(imagesResource, c.ns, name), &v1alpha1.Image{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Image), err
}

// List takes label and field selectors, and returns the list of Images that match those selectors.
func (c *FakeImages) List(opts v1.ListOptions) (result *v1alpha1.ImageList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(imagesResource, imagesKind, c.ns, opts), &v1alpha1.ImageList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.ImageList{ListMeta: obj.(*v1alpha1.ImageList).ListMeta}
	for _, item := range obj.(*v1alpha1.ImageList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested images.
func (c *FakeImages) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(imagesResource, c.ns, opts))

}

// Create takes the representation of a image and creates it.  Returns the server's representation of the image, and an error, if there is any.
func (c *FakeImages) Create(image *v1alpha1.Image) (result *v1alpha1.Image, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(imagesResource, c.ns, image), &v1alpha1.Image{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Image), err
}

// Update takes the representation of a image and updates it. Returns the server's representation of the image, and an error, if there is any.
func (c *FakeImages) Update(image *v1alpha1.Image) (result *v1alpha1.Image, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(imagesResource, c.ns, image), &v1alpha1.Image{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Image), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeImages) UpdateStatus(image *v1alpha1.Image) (*v1alpha1.Image, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(imagesResource, "status", c.ns, image), &v1alpha1.Image{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Image), err
}

// Delete takes name of the image and deletes it. Returns an error if one occurs.
func (c *FakeImages) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(imagesResource, c.ns, name), &v1alpha1.Image{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeImages) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(imagesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v1alpha1.ImageList{})
	return err
}

// Patch applies the patch and returns the patched image.
func (c *FakeImages) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.Image, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(imagesResource, c.ns, name, data, subresources...), &v1alpha1.Image{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Image), err
}
