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

package v2

import (
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/clock"
	"knative.dev/pkg/kmeta"

	"knative.dev/pkg/tracker"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
)

// Accessor defines an abstraction for manipulating labeled entity
// (Configuration, Revision) with shared logic.
type Accessor interface {
	list(ns, routeName string, state v1.RoutingState) ([]kmeta.Accessor, error)
	patch(ns, name string, pt types.PatchType, p []byte) error
	makeMetadataPatch(ns, name, routeName string, remove bool) (map[string]interface{}, error)
}

// Revision is an implementation of Accessor for Revisions.
type Revision struct {
	client         clientset.Interface
	tracker        tracker.Interface
	revisionLister listers.RevisionLister
	clock          clock.Clock
}

// Revision implements Accessor
var _ Accessor = (*Revision)(nil)

// NewRevisionAccessor is a factory function to make a new revision accessor.
func NewRevisionAccessor(
	client clientset.Interface,
	tracker tracker.Interface,
	lister listers.RevisionLister,
	clock clock.Clock) *Revision {
	return &Revision{
		client:         client,
		tracker:        tracker,
		revisionLister: lister,
		clock:          clock,
	}
}

// makeMetadataPatch makes a metadata map to be patched or nil if no changes are needed.
func makeMetadataPatch(
	acc kmeta.Accessor, routeName string, addRoutingState, remove bool, clock clock.Clock) (map[string]interface{}, error) {
	labels := map[string]interface{}{}
	annotations := map[string]interface{}{}

	if stateChanged := addRouteAnnotation(acc, routeName, annotations, remove); stateChanged && addRoutingState {
		markRoutingState(acc, routeName != "", clock, labels, annotations)
	}

	meta := map[string]interface{}{}
	if len(labels) > 0 {
		meta["labels"] = labels
	}
	if len(annotations) > 0 {
		meta["annotations"] = annotations
	}
	if len(meta) > 0 {
		return map[string]interface{}{"metadata": meta}, nil
	}
	return nil, nil
}

// markRoutingState updates the RoutingStateLabel and bumps the modified time annotation.
func markRoutingState(
	acc kmeta.Accessor, hasRoute bool, clock clock.Clock,
	diffLabels, diffAnn map[string]interface{}) {
	wantState := string(v1.RoutingStateReserve)
	if hasRoute {
		wantState = string(v1.RoutingStateActive)
	}

	if acc.GetLabels()[serving.RoutingStateLabelKey] != wantState {
		diffLabels[serving.RoutingStateLabelKey] = wantState
		diffAnn[serving.RoutingStateModifiedAnnotationKey] = v1.RoutingStateModifiedString(clock)
	}
}

// addRouteAnnotation appends the route annotation to the list of labels if needed
// or removes the annotation if routeName is nil.
func addRouteAnnotation(acc kmeta.Accessor, routeName string, diffAnn map[string]interface{}, remove bool) bool {
	v, has := getListAnnValue(acc.GetAnnotations(), serving.RouteLabelKey, routeName)
	switch {
	case has && remove:
		if len(v) == 1 {
			diffAnn[serving.RouteLabelKey] = nil
			return true
		}
		s := removeFromList(v, routeName)
		diffAnn[serving.RouteLabelKey] = strings.Join(s, ",")
		return false

	case !has && !remove:
		if len(v) == 0 {
			diffAnn[serving.RouteLabelKey] = routeName
			return true
		}
		diffAnn[serving.RouteLabelKey] = strings.Join(v, ",") + "," + routeName
		return false
	}

	return false
}

func removeFromList(strs []string, s string) []string {
	l := len(strs)
	for i := 0; i < l; i++ {
		if strs[i] == s {
			strs[i], strs[l-1] = strs[l-1], strs[i]
			break
		}
	}
	return strs[:l-1]
}

// list implements Accessor
func (r *Revision) list(ns, routeName string, state v1.RoutingState) ([]kmeta.Accessor, error) {
	rl, err := r.revisionLister.Revisions(ns).List(labels.SelectorFromSet(labels.Set{
		serving.RoutingStateLabelKey: string(state),
	}))
	if err != nil {
		return nil, err
	}
	// Need a copy to change types in Go
	kl := make([]kmeta.Accessor, len(rl))
	for i, r := range rl {
		kl[i] = r
	}
	return kl, err
}

// patch implements Accessor
func (r *Revision) patch(ns, name string, pt types.PatchType, p []byte) error {
	_, err := r.client.ServingV1().Revisions(ns).Patch(name, pt, p)
	return err
}

func (r *Revision) makeMetadataPatch(ns, name, routeName string, remove bool) (map[string]interface{}, error) {
	rev, err := r.revisionLister.Revisions(ns).Get(name)
	if err != nil {
		return nil, err
	}
	return makeMetadataPatch(rev, routeName, true, remove, r.clock)
}

// Configuration is an implementation of Accessor for Configurations.
type Configuration struct {
	client              clientset.Interface
	tracker             tracker.Interface
	configurationLister listers.ConfigurationLister
	clock               clock.Clock
}

// Configuration implements Accessor
var _ Accessor = (*Configuration)(nil)

// NewConfigurationAccessor is a factory function to make a new configuration Accessor.
func NewConfigurationAccessor(
	client clientset.Interface,
	tracker tracker.Interface,
	lister listers.ConfigurationLister,
	clock clock.Clock) *Configuration {
	return &Configuration{
		client:              client,
		tracker:             tracker,
		configurationLister: lister,
		clock:               clock,
	}
}

// list implements Accessor
func (c *Configuration) list(ns, routeName string, state v1.RoutingState) ([]kmeta.Accessor, error) {
	// TODO(whaught): Do we want routingState labels on Configurations? Maybe
	cl, err := c.configurationLister.Configurations(ns).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	// Need a copy to change types in Go
	kl := make([]kmeta.Accessor, 0, len(cl))
	for _, c := range cl {
		if _, has := getListAnnValue(c.Annotations, serving.RouteLabelKey, routeName); has {
			kl = append(kl, c)
		}
	}
	return kl, err
}

func getListAnnValue(annotations map[string]string, key, value string) ([]string, bool) {
	l, ok := annotations[key]
	if ok {
		vals := strings.Split(l, ",")
		for _, r := range vals {
			if r == value {
				return vals, true
			}
		}
		return vals, false
	}
	return []string{}, false
}

// patch implements Accessor
func (c *Configuration) patch(ns, name string, pt types.PatchType, p []byte) error {
	_, err := c.client.ServingV1().Configurations(ns).Patch(name, pt, p)
	return err
}

func (c *Configuration) makeMetadataPatch(ns, name, routeName string, remove bool) (map[string]interface{}, error) {
	config, err := c.configurationLister.Configurations(ns).Get(name)
	if err != nil {
		return nil, err
	}
	return makeMetadataPatch(config, routeName, false, remove, c.clock)
}
