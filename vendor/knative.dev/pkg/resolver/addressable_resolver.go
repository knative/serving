/*
Copyright 2019 The Knative Authors

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

package resolver

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"

	"knative.dev/pkg/client/injection/ducks/duck/v1/addressable"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/network"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"

	"knative.dev/pkg/apis"
	pkgapisduck "knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	duckv1beta1 "knative.dev/pkg/apis/duck/v1beta1"
	"knative.dev/pkg/tracker"
)

// RefResolverFunc resolves ObjectReferences into a URI.
// It returns true when it handled the reference, in which case it also returns the resolved URI or an error.
type RefResolverFunc func(ctx context.Context, ref *corev1.ObjectReference) (bool, *apis.URL, error)

// URIResolver resolves Destinations and ObjectReferences into a URI.
type URIResolver struct {
	tracker       tracker.Interface
	listerFactory func(schema.GroupVersionResource) (cache.GenericLister, error)
	resolvers     []RefResolverFunc
}

// NewURIResolverFromTracker constructs a new URIResolver with context, a tracker and an optional list of custom resolvers.
func NewURIResolverFromTracker(ctx context.Context, t tracker.Interface, resolvers ...RefResolverFunc) *URIResolver {
	ret := &URIResolver{
		tracker:   t,
		resolvers: resolvers,
	}

	informerFactory := &pkgapisduck.CachedInformerFactory{
		Delegate: &pkgapisduck.EnqueueInformerFactory{
			Delegate:     addressable.Get(ctx),
			EventHandler: controller.HandleAll(ret.tracker.OnChanged),
		},
	}

	ret.listerFactory = func(gvr schema.GroupVersionResource) (cache.GenericLister, error) {
		_, l, err := informerFactory.Get(ctx, gvr)
		return l, err
	}

	return ret
}

// URIFromDestination resolves a v1beta1.Destination into a URI string.
func (r *URIResolver) URIFromDestination(ctx context.Context, dest duckv1beta1.Destination, parent interface{}) (string, error) {
	var deprecatedObjectReference *duckv1.KReference
	if !(dest.DeprecatedAPIVersion == "" && dest.DeprecatedKind == "" && dest.DeprecatedName == "" && dest.DeprecatedNamespace == "") {
		deprecatedObjectReference = &duckv1.KReference{
			Kind:       dest.DeprecatedKind,
			APIVersion: dest.DeprecatedAPIVersion,
			Name:       dest.DeprecatedName,
			Namespace:  dest.DeprecatedNamespace,
		}
	}
	if dest.Ref != nil && deprecatedObjectReference != nil {
		return "", errors.New("ref and [apiVersion, kind, name] can't be both present")
	}

	var ref *duckv1.KReference
	if dest.Ref != nil {
		ref = &duckv1.KReference{
			Kind:       dest.Ref.Kind,
			Namespace:  dest.Ref.Namespace,
			Name:       dest.Ref.Name,
			APIVersion: dest.Ref.APIVersion,
		}
	} else {
		ref = deprecatedObjectReference
	}

	u, err := r.URIFromDestinationV1(ctx, duckv1.Destination{Ref: ref, URI: dest.URI, CACerts: dest.CACerts}, parent)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// URIFromDestinationV1 resolves a v1.Destination into a URL.
func (r *URIResolver) URIFromDestinationV1(ctx context.Context, dest duckv1.Destination, parent interface{}) (*apis.URL, error) {
	addr, err := r.AddressableFromDestinationV1(ctx, dest, parent)
	if err != nil {
		return nil, err
	}
	return addr.URL, nil
}

func (r *URIResolver) URIFromKReference(ctx context.Context, ref *duckv1.KReference, parent interface{}) (*apis.URL, error) {
	dest := duckv1.Destination{
		Ref: ref,
	}
	addr, err := r.AddressableFromDestinationV1(ctx, dest, parent)
	if err != nil {
		return nil, err
	}
	return addr.URL, nil
}

// URIFromObjectReference resolves an ObjectReference to a URI string.
func (r *URIResolver) URIFromObjectReference(ctx context.Context, ref *corev1.ObjectReference, parent interface{}) (*apis.URL, error) {
	if ref == nil {
		return nil, apierrs.NewBadRequest("ref is nil")
	}
	dest := duckv1.KReference{
		Kind:       ref.Kind,
		Namespace:  ref.Namespace,
		Name:       ref.Name,
		APIVersion: ref.APIVersion,
	}
	return r.URIFromKReference(ctx, &dest, parent)
}

// AddressableFromDestinationV1 resolves a v1.Destination into a duckv1.Addressable.
func (r *URIResolver) AddressableFromDestinationV1(ctx context.Context, dest duckv1.Destination, parent interface{}) (*duckv1.Addressable, error) {
	if dest.Ref != nil {
		addr, err := r.addressableFromDestinationRef(ctx, dest, parent)
		if err != nil {
			return nil, err
		}
		if dest.URI != nil {
			if dest.URI.URL().IsAbs() {
				return nil, errors.New("absolute URI is not allowed when Ref or [apiVersion, kind, name] exists")
			}
			addr.URL = addr.URL.ResolveReference(dest.URI)
			return addr, nil
		}
		return addr, nil
	}

	if dest.URI != nil {
		// IsAbs check whether the URL has a non-empty scheme. Besides the non non-empty scheme, we also require dest.URI has a non-empty host
		if !dest.URI.URL().IsAbs() || dest.URI.Host == "" {
			return nil, fmt.Errorf("URI is not absolute (both scheme and host should be non-empty): %q", dest.URI.String())
		}
		return &duckv1.Addressable{
			URL:      dest.URI,
			CACerts:  dest.CACerts,
			Audience: dest.Audience,
		}, nil
	}

	return nil, errors.New("destination missing Ref and URI, expected at least one")
}

func (r *URIResolver) addressableFromDestinationRef(ctx context.Context, dest duckv1.Destination, parent interface{}) (*duckv1.Addressable, error) {
	if dest.Ref == nil {
		return nil, apierrs.NewBadRequest("ref is nil")
	}

	or := &corev1.ObjectReference{
		Kind:       dest.Ref.Kind,
		Namespace:  dest.Ref.Namespace,
		Name:       dest.Ref.Name,
		APIVersion: dest.Ref.APIVersion,
	}

	// try custom resolvers first
	for _, resolver := range r.resolvers {
		handled, url, err := resolver(ctx, or)
		if handled {
			return &duckv1.Addressable{
				Name:     dest.Ref.Address,
				URL:      url,
				CACerts:  dest.CACerts,
				Audience: dest.Audience,
			}, err
		}

		// when handled is false, both url and err are ignored.
	}

	gvr, _ := meta.UnsafeGuessKindToResource(or.GroupVersionKind())
	if err := r.tracker.TrackReference(tracker.Reference{
		APIVersion: dest.Ref.APIVersion,
		Kind:       dest.Ref.Kind,
		Namespace:  dest.Ref.Namespace,
		Name:       dest.Ref.Name,
	}, parent); err != nil {
		return nil, fmt.Errorf("failed to track reference %s %s/%s: %w", gvr.String(), dest.Ref.Namespace, dest.Ref.Name, err)
	}

	lister, err := r.listerFactory(gvr)
	if err != nil {
		return nil, fmt.Errorf("failed to get lister for %s: %w", gvr.String(), err)
	}

	obj, err := lister.ByNamespace(dest.Ref.Namespace).Get(dest.Ref.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get object %s/%s: %w", dest.Ref.Namespace, dest.Ref.Name, err)
	}

	// K8s Services are special cased. They can be called, even though they do not satisfy the
	// Callable interface.
	if dest.Ref.APIVersion == "v1" && dest.Ref.Kind == "Service" {
		url := &apis.URL{
			Scheme: "http",
			Host:   network.GetServiceHostname(dest.Ref.Name, dest.Ref.Namespace),
			Path:   "",
		}
		if dest.CACerts != nil && *dest.CACerts != "" {
			url.Scheme = "https"
		}
		return &duckv1.Addressable{
			Name:     dest.Ref.Address,
			URL:      url,
			CACerts:  dest.CACerts,
			Audience: dest.Audience,
		}, nil
	}

	addressable, ok := obj.(*duckv1.AddressableType)
	if !ok {
		return nil, apierrs.NewBadRequest(fmt.Sprintf("%s(%T) is not an AddressableType", dest.Ref, dest.Ref))
	}

	addr, err := r.selectAddress(dest, addressable)
	if err != nil {
		return nil, err
	}
	// Do not modify informer copy.
	addr = addr.DeepCopy()

	if addr.URL == nil {
		return nil, apierrs.NewBadRequest(fmt.Sprintf("URL missing in address of %s", dest.Ref))
	}
	if addr.URL.Host == "" {
		return nil, apierrs.NewBadRequest(fmt.Sprintf("hostname missing in address of %s", dest.Ref))
	}

	if dest.CACerts != nil && *dest.CACerts != "" {
		addr.CACerts = dest.CACerts
	}

	if dest.Audience != nil && *dest.Audience != "" {
		// destinations audience takes preference
		addr.Audience = dest.Audience
	}

	return addr, nil
}

// selectAddress selects a single address from the given AddressableType
func (r *URIResolver) selectAddress(dest duckv1.Destination, addressable *duckv1.AddressableType) (*duckv1.Addressable, error) {
	if len(addressable.Status.Addresses) == 0 && addressable.Status.Address == nil {
		return nil, apierrs.NewBadRequest(fmt.Sprintf("address not set for %s", dest.Ref))
	}

	// if dest.ref.address is specified:
	// - select the first (in order) address from status.addresses with name == dest.ref.address
	// - If no address is found return an error explaining that the address with name dest.ref.address
	//   cannot be found/resolved
	if dest.Ref.Address != nil && *dest.Ref.Address != "" {
		for _, addr := range addressable.Status.Addresses {
			if addr.Name != nil && *addr.Name == *dest.Ref.Address {
				return &addr, nil
			}
		}
		return nil, apierrs.NewBadRequest(fmt.Sprintf("address with name %q not found for %s", *dest.Ref.Address, dest.Ref))
	}

	// if dest.ref.address is not specified:
	// - select the first (in order) address from status.addresses
	// - If no address is found in addresses use status.address
	if len(addressable.Status.Addresses) > 0 {
		return &addressable.Status.Addresses[0], nil
	}

	return addressable.Status.Address, nil
}
