/*
Copyright 2024 The Knative Authors

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
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"knative.dev/pkg/client/injection/ducks/duck/v1/authstatus"
	"knative.dev/pkg/controller"

	pkgapisduck "knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/tracker"
)

// AuthenticatableResolver resolves ObjectReferences into a AuthenticatableType.
type AuthenticatableResolver struct {
	tracker       tracker.Interface
	listerFactory func(schema.GroupVersionResource) (cache.GenericLister, error)
}

// NewAuthenticatableResolverFromTracker constructs a new AuthenticatableResolver with context and a tracker.
func NewAuthenticatableResolverFromTracker(ctx context.Context, t tracker.Interface) *AuthenticatableResolver {
	ret := &AuthenticatableResolver{
		tracker: t,
	}

	informerFactory := &pkgapisduck.CachedInformerFactory{
		Delegate: &pkgapisduck.EnqueueInformerFactory{
			Delegate:     authstatus.Get(ctx),
			EventHandler: controller.HandleAll(ret.tracker.OnChanged),
		},
	}

	ret.listerFactory = func(gvr schema.GroupVersionResource) (cache.GenericLister, error) {
		_, l, err := informerFactory.Get(ctx, gvr)
		return l, err
	}

	return ret
}

// AuthStatusFromObjectReference returns the AuthStatus from an object
func (r *AuthenticatableResolver) AuthStatusFromObjectReference(ref *corev1.ObjectReference, parent interface{}) (*duckv1.AuthStatus, error) {
	if ref == nil {
		return nil, apierrs.NewBadRequest("ref is nil")
	}

	authenticatable, err := r.authenticatableFromObjectReference(ref, parent)
	if err != nil {
		return nil, fmt.Errorf("failed to get authenticatable %s/%s: %w", ref.Namespace, ref.Name, err)
	}

	if authenticatable.Status.Auth == nil {
		return nil, fmt.Errorf(".status.auth is missing in object %s/%s", ref.Namespace, ref.Name)
	}

	return authenticatable.Status.Auth, nil
}

// authenticatableFromObjectReference resolves an object reference into an AuthenticatableType
func (r *AuthenticatableResolver) authenticatableFromObjectReference(ref *corev1.ObjectReference, parent interface{}) (*duckv1.AuthenticatableType, error) {
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
		return nil, fmt.Errorf("failed to track reference %s %s/%s: %w", gvr.String(), ref.Namespace, ref.Name, err)
	}

	lister, err := r.listerFactory(gvr)
	if err != nil {
		return nil, fmt.Errorf("failed to get lister for %s: %w", gvr.String(), err)
	}

	obj, err := lister.ByNamespace(ref.Namespace).Get(ref.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get object %s/%s: %w", ref.Namespace, ref.Name, err)
	}

	authenticatable, ok := obj.(*duckv1.AuthenticatableType)
	if !ok {
		return nil, apierrs.NewBadRequest(fmt.Sprintf("%s(%T) is not an AuthenticatableType", ref, ref))
	}

	// Do not modify informer copy.
	authenticatable = authenticatable.DeepCopy()

	return authenticatable, nil
}
