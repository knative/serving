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

package v1

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/tracker"

	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// SyncLabels makes sure that the revisions and configurations referenced from
// a Route are labeled with route labels.
func SyncLabels(ctx context.Context, r *v1.Route, cacc *Configuration, racc *Revision) error {
	revisions := sets.NewString()
	configs := sets.NewString()

	// Walk the Route's .status.traffic and .spec.traffic and build a list
	// of revisions and configurations to label
	for _, tt := range append(r.Status.Traffic, r.Spec.Traffic...) {
		revName := tt.RevisionName
		configName := tt.ConfigurationName

		if revName != "" {
			if err := racc.tracker.TrackReference(ref(r.Namespace, revName, "Revision"), r); err != nil {
				return err
			}

			rev, err := racc.get(ctx, r.Namespace, revName)
			if err != nil {
				// The revision might not exist (yet). The informers will notify if it gets created.
				continue
			}

			revisions.Insert(revName)

			// If the owner reference is a configuration, treat it like a configuration target
			if owner := metav1.GetControllerOf(rev); owner != nil && owner.Kind == "Configuration" {
				configName = owner.Name
			}
		}

		if configName != "" {
			if err := cacc.tracker.TrackReference(ref(r.Namespace, configName, "Configuration"), r); err != nil {
				return err
			}

			config, err := cacc.configurationLister.Configurations(r.Namespace).Get(configName)
			if err != nil {
				// The config might not exist (yet). The informers will notify if it gets created.
				continue
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
	if err := deleteLabelForNotListed(ctx, r.Namespace, r.Name, racc, revisions); err != nil {
		return err
	}
	if err := setLabelForListed(ctx, r, racc, revisions); err != nil {
		return err
	}

	// Use a config access to manipulate the configs.
	if err := deleteLabelForNotListed(ctx, r.Namespace, r.Name, cacc, configs); err != nil {
		return err
	}
	return setLabelForListed(ctx, r, cacc, configs)
}

// ClearLabels removes any labels for a named route from given accessors.
func ClearLabels(ctx context.Context, ns, name string, accs ...Accessor) error {
	for _, acc := range accs {
		if err := deleteLabelForNotListed(ctx, ns, name, acc, nil /*none listed*/); err != nil {
			return err
		}
	}
	return nil
}

// setLabelForListed uses the accessor to attach the label for this route to every element
// listed within "names" in the same namespace.
func setLabelForListed(ctx context.Context, route *v1.Route, acc Accessor, names sets.String) error {
	for name := range names {
		elt, err := acc.get(ctx, route.Namespace, name)
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
			if err := setRouteLabel(ctx, acc, elt, &route.Name); err != nil {
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
func deleteLabelForNotListed(ctx context.Context, ns, name string, acc Accessor, names sets.String) error {
	oldList, err := acc.list(ctx, ns, name)
	if err != nil {
		return err
	}

	// Delete label for newly removed traffic targets.
	for _, elt := range oldList {
		if names.Has(elt.GetName()) {
			continue
		}

		if err := setRouteLabel(ctx, acc, elt, nil); err != nil {
			return fmt.Errorf("failed to remove route label to %s %q: %w",
				elt.GroupVersionKind(), elt.GetName(), err)
		}
	}

	return nil
}

// setRouteLabel toggles the route label on the specified element through the provided accessor.
// a nil route name will cause the route label to be deleted, and a non-nil route will cause
// that route name to be attached to the element.
func setRouteLabel(ctx context.Context, acc Accessor, elt kmeta.Accessor, routeName *string) error {
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

	return acc.patch(ctx, elt.GetNamespace(), elt.GetName(), types.MergePatchType, patch)
}

func ref(namespace, name, kind string) tracker.Reference {
	apiVersion, kind := v1.SchemeGroupVersion.WithKind(kind).ToAPIVersionAndKind()
	return tracker.Reference{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespace,
	}
}
