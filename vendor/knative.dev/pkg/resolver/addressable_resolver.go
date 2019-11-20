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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"

	"knative.dev/pkg/apis"
	pkgapisduck "knative.dev/pkg/apis/duck"
	duckv1beta1 "knative.dev/pkg/apis/duck/v1beta1"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/network"
	"knative.dev/pkg/tracker"

	"knative.dev/pkg/injection/clients/dynamicclient"
)

// URIResolver resolves Destinations and ObjectReferences into a URI.
type URIResolver struct {
	tracker         tracker.Interface
	informerFactory pkgapisduck.InformerFactory
}

// NewURIResolver constructs a new URIResolver with context and a callback passed to the URIResolver's tracker.
func NewURIResolver(ctx context.Context, callback func(types.NamespacedName)) *URIResolver {
	ret := &URIResolver{}

	ret.tracker = tracker.New(callback, controller.GetTrackerLease(ctx))
	ret.informerFactory = &pkgapisduck.CachedInformerFactory{
		Delegate: &pkgapisduck.EnqueueInformerFactory{
			Delegate: &pkgapisduck.TypedInformerFactory{
				Client:       dynamicclient.Get(ctx),
				Type:         &duckv1beta1.AddressableType{},
				ResyncPeriod: controller.GetResyncPeriod(ctx),
				StopChannel:  ctx.Done(),
			},
			EventHandler: controller.HandleAll(ret.tracker.OnChanged),
		},
	}

	return ret
}

// URIFromDestination resolves a Destination into a URI string.
func (r *URIResolver) URIFromDestination(dest duckv1beta1.Destination, parent interface{}) (string, error) {
	var deprecatedObjectReference *corev1.ObjectReference
	if dest.DeprecatedAPIVersion == "" && dest.DeprecatedKind == "" && dest.DeprecatedName == "" && dest.DeprecatedNamespace == "" {
		deprecatedObjectReference = nil
	} else {
		deprecatedObjectReference = &corev1.ObjectReference{
			Kind:       dest.DeprecatedKind,
			APIVersion: dest.DeprecatedAPIVersion,
			Name:       dest.DeprecatedName,
			Namespace:  dest.DeprecatedNamespace,
		}
	}
	if dest.Ref != nil && deprecatedObjectReference != nil {
		return "", fmt.Errorf("ref and [apiVersion, kind, name] can't be both present")
	}
	var ref *corev1.ObjectReference
	if dest.Ref != nil {
		ref = dest.Ref
	} else {
		ref = deprecatedObjectReference
	}
	if ref != nil {
		url, err := r.URIFromObjectReference(ref, parent)
		if err != nil {
			return "", err
		}
		if dest.URI != nil {
			if dest.URI.URL().IsAbs() {
				return "", fmt.Errorf("absolute URI is not allowed when Ref or [apiVersion, kind, name] exists")
			}
			return url.URL().ResolveReference(dest.URI.URL()).String(), nil
		}
		return url.URL().String(), nil
	}

	if dest.URI != nil {
		// IsAbs check whether the URL has a non-empty scheme. Besides the non non-empty scheme, we also require dest.URI has a non-empty host
		if !dest.URI.URL().IsAbs() || dest.URI.Host == "" {
			return "", fmt.Errorf("URI is not absolute(both scheme and host should be non-empty): %v", dest.URI.String())
		}
		return dest.URI.String(), nil
	}

	return "", fmt.Errorf("destination missing Ref, [apiVersion, kind, name] and URI, expected at least one")
}

// URIFromObjectReference resolves an ObjectReference to a URI string.
func (r *URIResolver) URIFromObjectReference(ref *corev1.ObjectReference, parent interface{}) (*apis.URL, error) {
	if ref == nil {
		return nil, errors.New("ref is nil")
	}

	if err := r.tracker.Track(*ref, parent); err != nil {
		return nil, fmt.Errorf("failed to track %+v: %v", ref, err)
	}

	// K8s Services are special cased. They can be called, even though they do not satisfy the
	// Callable interface.
	// TODO(spencer-p,n3wscott) Verify that the service actually exists in K8s.
	if ref.APIVersion == "v1" && ref.Kind == "Service" {
		url := &apis.URL{
			Scheme: "http",
			Host:   ServiceHostName(ref.Name, ref.Namespace),
			Path:   "/",
		}
		return url, nil
	}

	gvr, _ := meta.UnsafeGuessKindToResource(ref.GroupVersionKind())
	_, lister, err := r.informerFactory.Get(gvr)
	if err != nil {
		return nil, fmt.Errorf("failed to get lister for %+v: %v", gvr, err)
	}

	obj, err := lister.ByNamespace(ref.Namespace).Get(ref.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get ref %+v: %v", ref, err)
	}

	addressable, ok := obj.(*duckv1beta1.AddressableType)
	if !ok {
		return nil, fmt.Errorf("%+v is not an AddressableType", ref)
	}
	if addressable.Status.Address == nil {
		return nil, fmt.Errorf("address not set for %+v", ref)
	}
	url := addressable.Status.Address.URL
	if url == nil {
		return nil, fmt.Errorf("url missing in address of %+v", ref)
	}
	if url.Host == "" {
		return nil, fmt.Errorf("hostname missing in address of %+v", ref)
	}
	return url, nil
}

// ServiceHostName resolves the hostname for a Kubernetes Service.
func ServiceHostName(serviceName, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.%s", serviceName, namespace, network.GetClusterDomainName())
}
