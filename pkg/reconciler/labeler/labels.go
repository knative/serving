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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// syncLabels makes sure that the revisions and configurations referenced from
// a Route are labeled with route labels.
func (c *Reconciler) syncLabels(ctx context.Context, r *v1.Route) error {
	revisions := sets.NewString()
	configs := sets.NewString()

	// Walk the Route's .status.traffic and .spec.traffic and build a list
	// of revisions and configurations to label
	for _, tt := range append(r.Status.Traffic, r.Spec.Traffic...) {
		revName := tt.RevisionName
		configName := tt.ConfigurationName

		if revName != "" {
			rev, err := c.revisionLister.Revisions(r.Namespace).Get(revName)
			if err != nil {
				return err
			}

			revisions.Insert(revName)

			// If the owner reference is a configuration, treat it like a configuration target
			if owner := metav1.GetControllerOf(rev); owner != nil && owner.Kind == "Configuration" {
				configName = owner.Name
			}
		}

		if configName != "" {
			config, err := c.configurationLister.Configurations(r.Namespace).Get(configName)
			if err != nil {
				return err
			}

			configs.Insert(configName)

			// If the target is for the latest revision, add the latest created revision to the list
			// so that there is a smooth transition when the new revision becomes ready.
			if config.Status.LatestCreatedRevisionName != "" && tt.LatestRevision != nil && *tt.LatestRevision {
				revisions.Insert(config.Status.LatestCreatedRevisionName)
			}
		}
	}

	// Use a revision accessor to manipulate the revisions.
	racc := &revision{r: c}
	if err := deleteLabelForNotListed(ctx, r.Namespace, r.Name, racc, revisions); err != nil {
		return err
	}
	if err := setLabelForListed(r, racc, revisions); err != nil {
		return err
	}

	// Use a config access to manipulate the configs.
	cacc := &configuration{r: c}
	if err := deleteLabelForNotListed(ctx, r.Namespace, r.Name, cacc, configs); err != nil {
		return err
	}
	return setLabelForListed(r, cacc, configs)
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
func setLabelForListed(route *v1.Route, acc accessor, names sets.String) error {
	for name := range names {
		if err := setRouteLabel(acc, route.Namespace, name, &route.Name); err != nil {
			return fmt.Errorf("failed to add route label to Namespace=%s %q: %w", route.Namespace, name, err)
		}
	}

	return nil
}

// deleteLabelForNotListed uses the accessor to delete the label from any listable entity that is
// not named within our list.  Unlike setLabelForListed, this function takes ns/name instead of a
// Route so that it can clean things up when a Route ceases to exist.
func deleteLabelForNotListed(ctx context.Context, ns, routeName string, acc accessor, names sets.String) error {
	oldList, err := acc.list(ns, routeName)
	if err != nil {
		return err
	}

	// Delete label for newly removed traffic targets.
	for _, elt := range oldList {
		name := elt.GetName()
		if names.Has(name) {
			continue
		}

		if err := setRouteLabel(acc, ns, name, nil); err != nil {
			return fmt.Errorf("failed to remove route label to %s %q: %w",
				elt.GroupVersionKind(), name, err)
		}
	}

	return nil
}

// setRouteLabel toggles the route label on the specified element through the provided accessor.
// a nil route name will cause the route label to be deleted, and a non-nil route will cause
// that route name to be attached to the element.
func setRouteLabel(acc accessor, ns, name string, routeName *string) error {
	if mergePatch, err := acc.makeMetadataPatch(ns, name, routeName); err != nil {
		return err
	} else if mergePatch != nil {
		if patch, err2 := json.Marshal(mergePatch); err2 != nil {
			return err2
		} else {
			return acc.patch(ns, name, types.MergePatchType, patch)
		}
	}
	return nil
}
