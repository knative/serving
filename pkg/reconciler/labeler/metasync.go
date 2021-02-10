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

package labeler

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/tracker"

	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// syncRoutingMeta makes sure that the revisions and configurations referenced from
// a Route are labeled with the routingState label and routes annotation.
func syncRoutingMeta(ctx context.Context, r *v1.Route, cacc *ConfigurationAcc, racc *RevisionAcc) error {
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

			rev, err := racc.lister.Revisions(r.Namespace).Get(revName)
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

			config, err := cacc.lister.Configurations(r.Namespace).Get(configName)
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

	// Clear old meta only after the route is fully resolved
	if r.IsReady() || r.IsFailed() {
		if err := clearMetaForNotListed(ctx, r, racc, revisions); err != nil {
			return err
		}
		if err := clearMetaForNotListed(ctx, r, cacc, configs); err != nil {
			return err
		}
	}

	if err := setMetaForListed(ctx, r, racc, revisions); err != nil {
		return err
	}
	return setMetaForListed(ctx, r, cacc, configs)
}

// clearRoutingMeta removes any labels for a named route from given accessors.
func clearRoutingMeta(ctx context.Context, r *v1.Route, accs ...Accessor) error {
	for _, acc := range accs {
		if err := clearMetaForNotListed(ctx, r, acc, nil /*none listed*/); err != nil {
			return err
		}
	}
	return nil
}

// setMetaForListed uses the accessor to attach the label for this route to every element
// listed within "names" in the same namespace.
func setMetaForListed(ctx context.Context, route *v1.Route, acc Accessor, names sets.String) error {
	for name := range names {
		if err := setRoutingMeta(ctx, acc, route, name, false); err != nil {
			return fmt.Errorf("failed to add route annotation to Namespace=%s Name=%q: %w", route.Namespace, name, err)
		}
	}
	return nil
}

// clearMetaForNotListed uses the accessor to delete the label from any listable entity that is
// not named within our list.  Unlike setMetaForListed, this function takes ns/name instead of a
// Route so that it can clean things up when a Route ceases to exist.
func clearMetaForNotListed(ctx context.Context, r *v1.Route, acc Accessor, names sets.String) error {
	oldList, err := acc.list(ctx, r.Namespace, r.Name, v1.RoutingStateActive)
	if err != nil {
		return err
	}

	// Delete label for newly removed traffic targets.
	for _, elt := range oldList {
		name := elt.GetName()
		if names.Has(name) {
			continue
		}

		if err := setRoutingMeta(ctx, acc, r, name, true); err != nil {
			return fmt.Errorf("failed to remove route annotation to %s %q: %w",
				elt.GroupVersionKind(), name, err)
		}
	}

	return nil
}

// setRoutingMeta toggles the routing state label, routes list and timestamp annotation on the specified
// element through the provided accessor.
// A nil route name will cause the route to be de-referenced, and a non-nil route will cause
// that route name to be attached to the element.
func setRoutingMeta(ctx context.Context, acc Accessor, r *v1.Route, name string, remove bool) error {
	if mergePatch, err := acc.makeMetadataPatch(r, name, remove); err != nil {
		return err
	} else if mergePatch != nil {
		patch, err := json.Marshal(mergePatch)
		if err != nil {
			return err
		}
		logger := logging.FromContext(ctx)
		logger.Debugf("Labeler V2 applying patch to %q. patch: %q", name, mergePatch)
		return acc.patch(ctx, r.Namespace, name, types.MergePatchType, patch)
	}

	return nil
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
