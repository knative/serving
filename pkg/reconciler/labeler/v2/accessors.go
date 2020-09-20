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

package v2

import (
	"context"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/client-go/tools/cache"
	"k8s.io/kube-openapi/pkg/util/sets"
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
	list(ctx context.Context, ns, routeName string, state v1.RoutingState) ([]kmeta.Accessor, error)
	patch(ctx context.Context, ns, name string, pt types.PatchType, p []byte) error
	makeMetadataPatch(ctx context.Context, route *v1.Route, name string, remove bool) (map[string]interface{}, error)
}

// Revision is an implementation of Accessor for Revisions.
type Revision struct {
	client  clientset.Interface
	tracker tracker.Interface
	lister  listers.RevisionLister
	indexer cache.Indexer
	clock   clock.Clock
}

// Revision implements Accessor
var _ Accessor = (*Revision)(nil)

// NewRevisionAccessor is a factory function to make a new revision accessor.
func NewRevisionAccessor(
	client clientset.Interface,
	tracker tracker.Interface,
	lister listers.RevisionLister,
	indexer cache.Indexer,
	clock clock.Clock) *Revision {
	return &Revision{
		client:  client,
		tracker: tracker,
		lister:  lister,
		indexer: indexer,
		clock:   clock,
	}
}

// makeMetadataPatch makes a metadata map to be patched or nil if no changes are needed.
func makeMetadataPatch(
	ctx context.Context,
	acc kmeta.Accessor, routeName string, addRoutingState, remove bool, clock clock.Clock) (map[string]interface{}, error) {
	labels := map[string]interface{}{}
	annotations := map[string]interface{}{}

	updateRouteAnnotation(ctx, acc, routeName, annotations, remove)

	if addRoutingState {
		markRoutingState(ctx, acc, clock, routeName, labels, annotations)
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
	ctx context.Context, acc kmeta.Accessor, clock clock.Clock,
	routeName string, diffLabels, diffAnn map[string]interface{}) {

	hasRoute := false
	if val, has := diffAnn[serving.RoutesAnnotationKey]; has {
		if val == nil {
			hasRoute = false
		} else {
			hasRoute = sets.NewString(strings.Split(val.(string), ",")...).Has(routeName)
		}
	} else {
		hasRoute = GetListAnnValue(acc.GetAnnotations(), serving.RoutesAnnotationKey).Has(routeName)
	}

	wantState := string(v1.RoutingStateReserve)
	if hasRoute {
		wantState = string(v1.RoutingStateActive)
	}

	if acc.GetLabels()[serving.RoutingStateLabelKey] != wantState {
		diffLabels[serving.RoutingStateLabelKey] = wantState
		diffAnn[serving.RoutingStateModifiedAnnotationKey] = v1.RoutingStateModifiedString(clock)
	}
}

// updateRouteAnnotation appends the route annotation to the list of labels if needed
// or removes the annotation if routeName is nil.
// Returns true if the entire annotation is newly added or removed, which signifies a state change.
func updateRouteAnnotation(ctx context.Context, acc kmeta.Accessor, routeName string, diffAnn map[string]interface{}, remove bool) {
	valSet := GetListAnnValue(acc.GetAnnotations(), serving.RoutesAnnotationKey)
	has := valSet.Has(routeName)
	switch {
	case has && remove:
		if len(valSet) == 1 {
			diffAnn[serving.RoutesAnnotationKey] = nil
			return
		}
		valSet.Delete(routeName)
		diffAnn[serving.RoutesAnnotationKey] = strings.Join(valSet.UnsortedList(), ",")

	case !has && !remove:
		if len(valSet) == 0 {
			diffAnn[serving.RoutesAnnotationKey] = routeName
			return
		}
		valSet.Insert(routeName)
		diffAnn[serving.RoutesAnnotationKey] = strings.Join(valSet.UnsortedList(), ",")
	}
}

// list implements Accessor
func (r *Revision) list(ctx context.Context, ns, routeName string, state v1.RoutingState) ([]kmeta.Accessor, error) {
	kl := make([]kmeta.Accessor, 0, 1)
	filter := func(m interface{}) {
		r := m.(*v1.Revision)
		if GetListAnnValue(r.Annotations, serving.RoutesAnnotationKey).Has(routeName) {
			kl = append(kl, r)
		}
	}
	selector := labels.SelectorFromSet(labels.Set{
		serving.RoutingStateLabelKey: string(state),
	})

	if err := cache.ListAllByNamespace(r.indexer, ns, selector, filter); err != nil {
		return nil, err
	}
	return kl, nil
}

// patch implements Accessor
func (r *Revision) patch(ctx context.Context, ns, name string, pt types.PatchType, p []byte) error {
	_, err := r.client.ServingV1().Revisions(ns).Patch(ctx, name, pt, p, metav1.PatchOptions{})
	return err
}

func (r *Revision) makeMetadataPatch(ctx context.Context, route *v1.Route, name string, remove bool) (map[string]interface{}, error) {
	rev, err := r.lister.Revisions(route.Namespace).Get(name)
	if err != nil {
		return nil, err
	}
	return makeMetadataPatch(ctx, rev, route.Name, true /*addRoutingState*/, remove, r.clock)
}

// Configuration is an implementation of Accessor for Configurations.
type Configuration struct {
	client  clientset.Interface
	tracker tracker.Interface
	lister  listers.ConfigurationLister
	indexer cache.Indexer
	clock   clock.Clock
}

// Configuration implements Accessor
var _ Accessor = (*Configuration)(nil)

// NewConfigurationAccessor is a factory function to make a new configuration Accessor.
func NewConfigurationAccessor(
	client clientset.Interface,
	tracker tracker.Interface,
	lister listers.ConfigurationLister,
	indexer cache.Indexer,
	clock clock.Clock) *Configuration {
	return &Configuration{
		client:  client,
		tracker: tracker,
		lister:  lister,
		indexer: indexer,
		clock:   clock,
	}
}

// list implements Accessor
func (c *Configuration) list(ctx context.Context, ns, routeName string, state v1.RoutingState) ([]kmeta.Accessor, error) {
	kl := make([]kmeta.Accessor, 0, 1)
	filter := func(m interface{}) {
		c := m.(*v1.Configuration)
		if GetListAnnValue(c.Annotations, serving.RoutesAnnotationKey).Has(routeName) {
			kl = append(kl, c)
		}
	}

	if err := cache.ListAllByNamespace(c.indexer, ns, labels.Everything(), filter); err != nil {
		return nil, err
	}
	return kl, nil
}

// GetListAnnValue finds a given value in a comma-separated annotation.
// returns the entire annotation value and true if found.
func GetListAnnValue(annotations map[string]string, key string) sets.String {
	l := annotations[key]
	if l == "" {
		return sets.String{}
	}
	return sets.NewString(strings.Split(l, ",")...)
}

// patch implements Accessor
func (c *Configuration) patch(ctx context.Context, ns, name string, pt types.PatchType, p []byte) error {
	_, err := c.client.ServingV1().Configurations(ns).Patch(ctx, name, pt, p, metav1.PatchOptions{})
	return err
}

func (c *Configuration) makeMetadataPatch(ctx context.Context, r *v1.Route, name string, remove bool) (map[string]interface{}, error) {
	config, err := c.lister.Configurations(r.Namespace).Get(name)
	if err != nil {
		return nil, err
	}
	return makeMetadataPatch(ctx, config, r.Name, false /*addRoutingState*/, remove, c.clock)
}
