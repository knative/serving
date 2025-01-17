/*
Copyright 2020 The Knative Authors

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

// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	gentype "k8s.io/client-go/gentype"
	v1alpha1 "knative.dev/caching/pkg/apis/caching/v1alpha1"
	scheme "knative.dev/caching/pkg/client/clientset/versioned/scheme"
)

// ImagesGetter has a method to return a ImageInterface.
// A group's client should implement this interface.
type ImagesGetter interface {
	Images(namespace string) ImageInterface
}

// ImageInterface has methods to work with Image resources.
type ImageInterface interface {
	Create(ctx context.Context, image *v1alpha1.Image, opts v1.CreateOptions) (*v1alpha1.Image, error)
	Update(ctx context.Context, image *v1alpha1.Image, opts v1.UpdateOptions) (*v1alpha1.Image, error)
	// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
	UpdateStatus(ctx context.Context, image *v1alpha1.Image, opts v1.UpdateOptions) (*v1alpha1.Image, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.Image, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.ImageList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.Image, err error)
	ImageExpansion
}

// images implements ImageInterface
type images struct {
	*gentype.ClientWithList[*v1alpha1.Image, *v1alpha1.ImageList]
}

// newImages returns a Images
func newImages(c *CachingV1alpha1Client, namespace string) *images {
	return &images{
		gentype.NewClientWithList[*v1alpha1.Image, *v1alpha1.ImageList](
			"images",
			c.RESTClient(),
			scheme.ParameterCodec,
			namespace,
			func() *v1alpha1.Image { return &v1alpha1.Image{} },
			func() *v1alpha1.ImageList { return &v1alpha1.ImageList{} }),
	}
}
