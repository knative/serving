/*
Copyright 2026 The Knative Authors

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

package servicescaler

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"knative.dev/serving/pkg/client/clientset/versioned"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/tracker"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
)

// Reconciler implements controller.Reconciler for Service resources.
type Reconciler struct {
	client versioned.Interface

	// listers index properties about resources
	revisionLister  listers.RevisionLister
	revisionIndexer cache.Indexer
	routeLister     listers.RouteLister

	tracker tracker.Interface
}

var (
	_ routereconciler.Finalizer = (*Reconciler)(nil)
	_ routereconciler.Interface = (*Reconciler)(nil)
)

// FinalizeKind removes service minscale from its traffic targets
// This does not modify or observe spec for the Route itself.
func (c *Reconciler) FinalizeKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	return c.removeServiceScaleAnnotations(ctx, r)
}

func (c *Reconciler) ReconcileKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	ctx, cancel := context.WithTimeout(ctx, pkgreconciler.DefaultTimeout)
	defer cancel()

	logger := logging.FromContext(ctx)

	for _, tt := range r.Status.Traffic {
		revName := tt.RevisionName
		if revName != "" {
			if err := c.tracker.TrackReference(ref(r.Namespace, revName, "Revision"), r); err != nil {
				return err
			}

			rev, err := c.revisionLister.Revisions(r.Namespace).Get(revName)
			if err != nil {
				// The revision might not exist (yet). The informers will notify if it gets created.
				continue
			}

			serviceMinScale := computeServiceMinScale(tt, r)
			revCopy := rev.DeepCopy()
			logger.Info("service min scale ", serviceMinScale)
			err = c.patchServiceScaleAnnotations(ctx, revCopy, r, serviceMinScale)
			if err != nil {
				logger.Error(err)
				return err
			}
		}
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

func computeServiceMinScale(tt v1.TrafficTarget, r *v1.Route) int32 {
	_, serviceMinScale, ok := serving.ServiceMinscaleAnnotation.Get(r.GetAnnotations())
	if ok && tt.Percent != nil {
		val, err := strconv.ParseFloat(serviceMinScale, 32)
		if err != nil {
			// should not happen, but just incase return 0
			return 0
		}
		return int32(math.Ceil((float64(*tt.Percent) / 100.0) * val))
	}

	// If annotation not found, assume 0
	return 0
}

func (c *Reconciler) patchServiceScaleAnnotations(ctx context.Context, rev *v1.Revision, route *v1.Route, serviceMinscale int32) error {
	routeKey := getRouteKey(route)
	_, val, minScaleOk := serving.ServiceMinscaleAnnotation.Get(rev.GetAnnotations())
	if minScaleOk {
		// min scale revision exists already, check if we need to update
		currentServiceMinScale, err := strconv.ParseInt(val, 10, 32)
		if err == nil && currentServiceMinScale == int64(serviceMinscale) {
			// if it is the same, no need to update
			return nil
		}

		_, routeName, routeNameOk := serving.ServiceMinscaleRouteAnnotation.Get(rev.GetAnnotations())
		// if not the same, check if there is already a route that applied service minscale to revision, if it exists
		if err == nil && routeNameOk && currentServiceMinScale > int64(serviceMinscale) {
			if routeName != "" && routeName != routeKey {
				// finally, check if route exists incase it previously failed deletion
				parts := strings.Split(routeName, "/")
				if len(parts) == 2 {
					_, err = c.routeLister.Routes(parts[0]).Get(parts[1])
					if err != nil && !apierrors.IsNotFound(err) {
						// if there is an error that is not a not found error, throw an error to reconcile again
						return err
					}

					if err == nil {
						// if no error, it means the route was found and no update is needed since route exists
						return nil
					}
				}
			}
		}
	}

	var serviceMinscalePatchVal *string
	var serviceMinscaleRoutePatchVal *string
	if serviceMinscale > 0 {
		// if greater than 0 apply service minscale
		// else apply blank annotation so that it gets removed
		formattedServiceMinscale := strconv.FormatInt(int64(serviceMinscale), 10)
		serviceMinscalePatchVal = &formattedServiceMinscale
		serviceMinscaleRoutePatchVal = &routeKey
	} else {
		serviceMinscalePatchVal = nil
		serviceMinscaleRoutePatchVal = nil
	}

	return c.patchRevisionServiceMinScale(ctx, route, rev.GetName(), serviceMinscalePatchVal, serviceMinscaleRoutePatchVal)
}

func (c *Reconciler) removeServiceScaleAnnotations(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	revAccessors, err := c.getRevAccessors(r)
	if err != nil {
		return err
	}

	for _, elt := range revAccessors {
		name := elt.GetName()
		if err := c.patchRevisionServiceMinScale(ctx, r, name, nil, nil); err != nil {
			return err
		}
	}

	return nil
}

func (c *Reconciler) patchRevisionServiceMinScale(ctx context.Context, route *v1.Route, revName string, serviceMinscale, serviceMinscaleRouteKey *string) error {
	annotationPatchMap := map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]any{
				serving.ServiceMinscaleAnnotation.Key():      serviceMinscale,
				serving.ServiceMinscaleRouteAnnotation.Key(): serviceMinscaleRouteKey,
			},
		},
	}

	patch, err := json.Marshal(annotationPatchMap)
	if err != nil {
		return err
	}

	_, err = c.client.ServingV1().Revisions(route.GetNamespace()).Patch(ctx, revName, types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

func (c *Reconciler) getRevAccessors(route *v1.Route) ([]kmeta.Accessor, error) {
	routeKey := getRouteKey(route)
	kl := make([]kmeta.Accessor, 0, 1)
	filter := func(m any) {
		rev := m.(*v1.Revision)
		// only target revision with minscale key associated with route
		if _, val, ok := serving.ServiceMinscaleRouteAnnotation.Get(rev.GetAnnotations()); ok && val == routeKey {
			kl = append(kl, rev)
		}
	}

	// get all in namespace
	selector := labels.SelectorFromSet(nil)
	if err := cache.ListAllByNamespace(c.revisionIndexer, route.GetNamespace(), selector, filter); err != nil {
		return nil, err
	}

	return kl, nil
}

func getRouteKey(route *v1.Route) string {
	return fmt.Sprintf("%s/%s", route.GetNamespace(), route.GetName())
}
