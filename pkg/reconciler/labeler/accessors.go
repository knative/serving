/*
Copyright 2020 The Knative Authors

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
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
)

// accessor defines an abstraction for manipulating labeled entity
// (Configuration, Revision) with shared logic.
type accessor interface {
	list(ns, name string) ([]kmeta.Accessor, error)
	patch(ns, name string, pt types.PatchType, p []byte) error
	makeMetadataPatch(ns, name string, routeName *string) (map[string]interface{}, error)
}

// makeMetadataPatch makes a metadata map to be patched or nil if no changes are needed.
func makeMetadataPatch(acc kmeta.Accessor, routeName *string) (map[string]interface{}, error) {
	labels := map[string]interface{}{}

	if err := addRouteLabel(acc, labels, routeName); err != nil {
		return nil, err
	}

	if len(labels) > 0 {
		meta := map[string]interface{}{
			"metadata": map[string]interface{}{
				"labels": labels,
			},
		}
		return meta, nil
	}
	return nil, nil
}

// addRouteLabel appends the route label to the list of labels if needed.
func addRouteLabel(acc kmeta.Accessor, labels map[string]interface{}, routeName *string) error {
	if oldLabels := acc.GetLabels(); oldLabels == nil && routeName != nil {
		labels[serving.RouteLabelKey] = routeName
	} else if oldLabel := oldLabels[serving.RouteLabelKey]; routeName == nil && oldLabel != "" {
		labels[serving.RouteLabelKey] = routeName
	} else if routeName != nil && oldLabel != *routeName {
		return fmt.Errorf("resource already has route label %q, and cannot be referenced by %q", oldLabel, *routeName)
	}

	return nil
}

// revision is an implementation of accessor for Revisions
type revision struct {
	r *Reconciler
}

// revision implements accessor
var _ accessor = (*revision)(nil)

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
	_, err := r.r.client.ServingV1().Revisions(ns).Patch(name, pt, p)
	return err
}

func (r *revision) makeMetadataPatch(ns, name string, routeName *string) (map[string]interface{}, error) {
	rev, err := r.r.revisionLister.Revisions(ns).Get(name)
	if err != nil {
		return nil, err
	}
	return makeMetadataPatch(rev, routeName)
}

// configuration is an implementation of accessor for Configurations
type configuration struct {
	r *Reconciler
}

// configuration implements accessor
var _ accessor = (*configuration)(nil)

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
	_, err := c.r.client.ServingV1().Configurations(ns).Patch(name, pt, p)
	return err
}

func (c *configuration) makeMetadataPatch(ns, name string, routeName *string) (map[string]interface{}, error) {
	config, err := c.r.configurationLister.Configurations(ns).Get(name)
	if err != nil {
		return nil, err
	}
	return makeMetadataPatch(config, routeName)
}
