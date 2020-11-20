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

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"

	"knative.dev/pkg/apis"
	pkgapisduck "knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	duckv1beta1 "knative.dev/pkg/apis/duck/v1beta1"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/network"
	"knative.dev/pkg/tracker"

	"knative.dev/pkg/client/injection/ducks/duck/v1/addressable"
)

// URIResolver resolves Destinations and ObjectReferences into a URI.
type URIResolver struct {
	tracker         tracker.Interface
	informerFactory pkgapisduck.InformerFactory
}

// NewURIResolver constructs a new URIResolver with context and a callback
// for a given listableType (Listable) passed to the URIResolver's tracker.
func NewURIResolver(ctx context.Context, callback func(types.NamespacedName)) *URIResolver {
	ret := &URIResolver{}

	ret.tracker = tracker.New(callback, controller.GetTrackerLease(ctx))
	ret.informerFactory = &pkgapisduck.CachedInformerFactory{
		Delegate: &pkgapisduck.EnqueueInformerFactory{
			Delegate:     addressable.Get(ctx),
			EventHandler: controller.HandleAll(ret.tracker.OnChanged),
		},
	}

	return ret
}

// URIFromDestination resolves a v1beta1.Destination into a URI string.
func (r *URIResolver) URIFromDestination(ctx context.Context, dest duckv1beta1.Destination, parent interface{}) (string, error) {
	var deprecatedObjectReference *corev1.ObjectReference
	if !(dest.DeprecatedAPIVersion == "" && dest.DeprecatedKind == "" && dest.DeprecatedName == "" && dest.DeprecatedNamespace == "") {
		deprecatedObjectReference = &corev1.ObjectReference{
			Kind:       dest.DeprecatedKind,
			APIVersion: dest.DeprecatedAPIVersion,
			Name:       dest.DeprecatedName,
			Namespace:  dest.DeprecatedNamespace,
		}
	}
	if dest.Ref != nil && deprecatedObjectReference != nil {
		return "", errors.New("ref and [apiVersion, kind, name] can't be both present")
	}
	var ref *corev1.ObjectReference
	if dest.Ref != nil {
		ref = dest.Ref
	} else {
		ref = deprecatedObjectReference
	}
	if ref != nil {
		url, err := r.URIFromObjectReference(ctx, ref, parent)
		if err != nil {
			return "", err
		}
		if dest.URI != nil {
			if dest.URI.URL().IsAbs() {
				return "", errors.New("absolute URI is not allowed when Ref or [apiVersion, kind, name] exists")
			}
			return url.ResolveReference(dest.URI).String(), nil
		}
		return url.URL().String(), nil
	}

	if dest.URI != nil {
		// IsAbs check whether the URL has a non-empty scheme. Besides the non non-empty scheme, we also require dest.URI has a non-empty host
		if !dest.URI.URL().IsAbs() || dest.URI.Host == "" {
			return "", fmt.Errorf("URI is not absolute (both scheme and host should be non-empty): %q", dest.URI.String())
		}
		return dest.URI.String(), nil
	}

	return "", errors.New("destination missing Ref, [apiVersion, kind, name] and URI, expected at least one")
}

// URIFromDestinationV1 resolves a v1.Destination into a URL.
func (r *URIResolver) URIFromDestinationV1(ctx context.Context, dest duckv1.Destination, parent interface{}) (*apis.URL, error) {
	if dest.Ref != nil {
		url, err := r.URIFromKReference(ctx, dest.Ref, parent)
		if err != nil {
			return nil, err
		}
		if dest.URI != nil {
			if dest.URI.URL().IsAbs() {
				return nil, errors.New("absolute URI is not allowed when Ref or [apiVersion, kind, name] exists")
			}
			return url.ResolveReference(dest.URI), nil
		}
		return url, nil
	}

	if dest.URI != nil {
		// IsAbs check whether the URL has a non-empty scheme. Besides the non non-empty scheme, we also require dest.URI has a non-empty host
		if !dest.URI.URL().IsAbs() || dest.URI.Host == "" {
			return nil, fmt.Errorf("URI is not absolute(both scheme and host should be non-empty): %q", dest.URI.String())
		}
		return dest.URI, nil
	}

	return nil, errors.New("destination missing Ref and URI, expected at least one")
}

func (r *URIResolver) URIFromKReference(ctx context.Context, ref *duckv1.KReference, parent interface{}) (*apis.URL, error) {
	return r.URIFromObjectReference(ctx, &corev1.ObjectReference{Name: ref.Name, Namespace: ref.Namespace, APIVersion: ref.APIVersion, Kind: ref.Kind}, parent)
}

// URIFromObjectReference resolves an ObjectReference to a URI string.
func (r *URIResolver) URIFromObjectReference(ctx context.Context, ref *corev1.ObjectReference, parent interface{}) (*apis.URL, error) {
	if ref == nil {
		return nil, apierrs.NewBadRequest("ref is nil")
	}

	gvr, _ := meta.UnsafeGuessKindToResource(ref.GroupVersionKind())
	if err := r.tracker.TrackReference(tracker.Reference{
		APIVersion: ref.APIVersion,
		Kind:       ref.Kind,
		Namespace:  ref.Namespace,
		Name:       ref.Name,
	}, parent); err != nil {
		return nil, apierrs.NewNotFound(gvr.GroupResource(), ref.Name)
	}

	// K8s Services are special cased. They can be called, even though they do not satisfy the
	// Callable interface.
	// TODO(spencer-p,n3wscott) Verify that the service actually exists in K8s.
	if ref.APIVersion == "v1" && ref.Kind == "Service" {
		url := &apis.URL{
			Scheme: "http",
			Host:   network.GetServiceHostname(ref.Name, ref.Namespace),
			Path:   "/",
		}
		return url, nil
	}

	_, lister, err := r.informerFactory.Get(ctx, gvr)
	if err != nil {
		return nil, apierrs.NewNotFound(gvr.GroupResource(), "Lister")
	}

	obj, err := lister.ByNamespace(ref.Namespace).Get(ref.Name)
	if err != nil {
		return nil, apierrs.NewNotFound(gvr.GroupResource(), ref.Name)
	}

	addressable, ok := obj.(*duckv1.AddressableType)
	if !ok {
		return nil, apierrs.NewBadRequest(fmt.Sprintf("%+v (%T) is not an AddressableType", ref, ref))
	}
	if addressable.Status.Address == nil {
		return nil, apierrs.NewBadRequest(fmt.Sprintf("address not set for %+v", ref))
	}
	url := addressable.Status.Address.URL
	if url == nil {
		return nil, apierrs.NewBadRequest(fmt.Sprintf("URL missing in address of %+v", ref))
	}
	if url.Host == "" {
		return nil, apierrs.NewBadRequest(fmt.Sprintf("hostname missing in address of %+v", ref))
	}
	return url, nil
}
