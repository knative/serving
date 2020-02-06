/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package labeler

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/pkg/kmeta"

	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
)

// accessor defines an abstraction for manipulating labeled entity
// (Configuration, Revision) with shared logic.
type accessor interface {
	get(ns, name string) (kmeta.Accessor, error)
	list(ns, name string) ([]kmeta.Accessor, error)
	patch(ns, name string, pt types.PatchType, p []byte) error
}

// syncLabels makes sure that the revisions and configurations referenced from
// a Route are labeled with route labels.
func (c *Reconciler) syncLabels(ctx context.Context, r *v1alpha1.Route) error {
	revisions := sets.NewString()
	configs := sets.NewString()

	// Walk the revisions in Route's .status.traffic and build a list
	// of Configurations to label from their OwnerReferences.
	for _, tt := range r.Status.Traffic {
		rev, err := c.revisionLister.Revisions(r.Namespace).Get(tt.RevisionName)
		if err != nil {
			return err
		}
		revisions.Insert(tt.RevisionName)
		owner := metav1.GetControllerOf(rev)
		if owner != nil && owner.Kind == "Configuration" {
			configs.Insert(owner.Name)
		}
	}

	// Use a revision accessor to manipulate the revisions.
	racc := &revision{r: c}
	if err := deleteLabelForNotListed(ctx, r.Namespace, r.Name, racc, revisions); err != nil {
		return err
	}
	if err := setLabelForListed(ctx, r, racc, revisions); err != nil {
		return err
	}

	// Use a config access to manipulate the configs.
	cacc := &configuration{r: c}
	if err := deleteLabelForNotListed(ctx, r.Namespace, r.Name, cacc, configs); err != nil {
		return err
	}
	return setLabelForListed(ctx, r, cacc, configs)
}

// clearLabels removes any labels for a named route from configurations and revisions.
func (c *Reconciler) clearLabels(ctx context.Context, ns, name string) error {
	racc := &revision{r: c}
	if err := deleteLabelForNotListed(ctx, ns, name, racc, sets.NewString()); err != nil {
		return err
	}
	cacc := &configuration{r: c}
	return deleteLabelForNotListed(ctx, ns, name, cacc, sets.NewString())
}

// setLabelForListed uses the accessor to attach the label for this route to every element
// listed within "names" in the same namespace.
func setLabelForListed(ctx context.Context, route *v1alpha1.Route, acc accessor, names sets.String) error {
	for name := range names {
		elt, err := acc.get(route.Namespace, name)
		if err != nil {
			return err
		}
		routeName, ok := elt.GetLabels()[serving.RouteLabelKey]
		if ok {
			if routeName != route.Name {
				return fmt.Errorf("%s %q is already in use by %q, and cannot be used by %q",
					elt.GroupVersionKind(), elt.GetName(), routeName, route.Name)
			}
		} else {
			if err := setRouteLabel(acc, elt, &route.Name); err != nil {
				return fmt.Errorf("failed to add route label to %s %q: %w",
					elt.GroupVersionKind(), elt.GetName(), err)
			}
		}
	}

	return nil
}

// deleteLabelForNotListed uses the accessor to delete the label from any listable entity that is
// not named within our list.  Unlike setLabelForListed, this function takes ns/name instead of a
// Route so that it can clean things up when a Route ceases to exist.
func deleteLabelForNotListed(ctx context.Context, ns, name string, acc accessor, names sets.String) error {
	oldList, err := acc.list(ns, name)
	if err != nil {
		return err
	}

	// Delete label for newly removed traffic targets.
	for _, elt := range oldList {
		if names.Has(elt.GetName()) {
			continue
		}

		if err := setRouteLabel(acc, elt, nil); err != nil {
			return fmt.Errorf("failed to remove route label to %s %q: %w",
				elt.GroupVersionKind(), elt.GetName(), err)
		}
	}

	return nil
}

// setRouteLabel toggles the route label on the specified element through the provided accessor.
// a nil route name will cause the route label to be deleted, and a non-nil route will cause
// that route name to be attached to the element.
func setRouteLabel(acc accessor, elt kmeta.Accessor, routeName *string) error {
	mergePatch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]interface{}{
				serving.RouteLabelKey: routeName,
			},
		},
	}

	patch, err := json.Marshal(mergePatch)
	if err != nil {
		return err
	}

	return acc.patch(elt.GetNamespace(), elt.GetName(), types.MergePatchType, patch)
}

// revision is an implementation of accessor for Revisions
type revision struct {
	r *Reconciler
}

// revision implements accessor
var _ accessor = (*revision)(nil)

// get implements accessor
func (r *revision) get(ns, name string) (kmeta.Accessor, error) {
	return r.r.revisionLister.Revisions(ns).Get(name)
}

// list implements accessor
func (r *revision) list(ns, name string) ([]kmeta.Accessor, error) {
	rl, err := r.r.revisionLister.Revisions(ns).List(labels.SelectorFromSet(labels.Set{
		serving.RouteLabelKey: name,
	}))
	if err != nil {
		return nil, err
	}
	// Need a copy to change types in Go
	kl := make([]kmeta.Accessor, 0, len(rl))
	for _, r := range rl {
		kl = append(kl, r)
	}
	return kl, err
}

// patch implements accessor
func (r *revision) patch(ns, name string, pt types.PatchType, p []byte) error {
	_, err := r.r.ServingClientSet.ServingV1alpha1().Revisions(ns).Patch(name, pt, p)
	return err
}

// configuration is an implementation of accessor for Configurations
type configuration struct {
	r *Reconciler
}

// configuration implements accessor
var _ accessor = (*configuration)(nil)

// get implements accessor
func (c *configuration) get(ns, name string) (kmeta.Accessor, error) {
	return c.r.configurationLister.Configurations(ns).Get(name)
}

// list implements accessor
func (c *configuration) list(ns, name string) ([]kmeta.Accessor, error) {
	rl, err := c.r.configurationLister.Configurations(ns).List(labels.SelectorFromSet(labels.Set{
		serving.RouteLabelKey: name,
	}))
	if err != nil {
		return nil, err
	}
	// Need a copy to change types in Go
	kl := make([]kmeta.Accessor, 0, len(rl))
	for _, r := range rl {
		kl = append(kl, r)
	}
	return kl, err
}

// patch implements accessor
func (c *configuration) patch(ns, name string, pt types.PatchType, p []byte) error {
	_, err := c.r.ServingClientSet.ServingV1alpha1().Configurations(ns).Patch(name, pt, p)
	return err
}
