/*
Copyright 2018 The Knative Authors.

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

package route

import (
	"context"
	"encoding/json"
	"golang.org/x/sync/errgroup"
	"reflect"

	"github.com/knative/pkg/apis/istio/v1alpha3"
	"github.com/knative/pkg/logging"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"github.com/knative/pkg/apis/duck"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources"
	resourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources/names"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/traffic"
)

func (c *Reconciler) reconcileVirtualService(ctx context.Context, route *v1alpha1.Route,
	desiredVirtualService *v1alpha3.VirtualService) error {
	logger := logging.FromContext(ctx)
	ns := desiredVirtualService.Namespace
	name := desiredVirtualService.Name

	virtualService, err := c.virtualServiceLister.VirtualServices(ns).Get(name)
	if apierrs.IsNotFound(err) {
		virtualService, err = c.SharedClientSet.NetworkingV1alpha3().VirtualServices(ns).Create(desiredVirtualService)
		if err != nil {
			logger.Error("Failed to create VirtualService", zap.Error(err))
			c.Recorder.Eventf(route, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create VirtualService %q: %v", name, err)
			return err
		}
		c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created",
			"Created VirtualService %q", desiredVirtualService.Name)
	} else if err != nil {
		return err
	} else if !equality.Semantic.DeepEqual(virtualService.Spec, desiredVirtualService.Spec) {
		// Don't modify the informers copy
		existing := virtualService.DeepCopy()
		existing.Spec = desiredVirtualService.Spec
		_, err = c.SharedClientSet.NetworkingV1alpha3().VirtualServices(ns).Update(existing)
		if err != nil {
			logger.Error("Failed to update VirtualService", zap.Error(err))
			return err
		}
	}

	// TODO(mattmoor): This is where we'd look at the state of the VirtualService and
	// reflect any necessary state into the Route.
	return err
}

func (c *Reconciler) reconcilePlaceholderService(ctx context.Context, route *v1alpha1.Route) error {
	logger := logging.FromContext(ctx)
	ns := route.Namespace
	name := resourcenames.K8sService(route)

	service, err := c.serviceLister.Services(ns).Get(name)
	if apierrs.IsNotFound(err) {
		// Doesn't exist, create it.
		desiredService := resources.MakeK8sService(route)
		service, err = c.KubeClientSet.CoreV1().Services(route.Namespace).Create(desiredService)
		if err != nil {
			logger.Error("Failed to create service", zap.Error(err))
			c.Recorder.Eventf(route, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create service %q: %v", name, err)
			return err
		}
		logger.Infof("Created service %s", name)
		c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created service %q", name)
	} else if err != nil {
		return err
	} else {
		// Make sure that the service has the proper specification
		desiredService := resources.MakeK8sService(route)
		// Preserve the ClusterIP field in the Service's Spec, if it has been set.
		desiredService.Spec.ClusterIP = service.Spec.ClusterIP
		if !equality.Semantic.DeepEqual(service.Spec, desiredService.Spec) {
			// Don't modify the informers copy
			existing := service.DeepCopy()
			existing.Spec = desiredService.Spec
			_, err = c.KubeClientSet.CoreV1().Services(ns).Update(existing)
			if err != nil {
				return err
			}
		}
	}
	// Update the information that makes us Targetable.
	route.Status.DomainInternal = resourcenames.K8sServiceFullname(route)
	route.Status.Targetable = &duckv1alpha1.Targetable{
		DomainInternal: resourcenames.K8sServiceFullname(route),
	}

	// TODO(mattmoor): This is where we'd look at the state of the Service and
	// reflect any necessary state into the Route.
	return nil
}

// Update the Status of the route.  Caller is responsible for checking
// for semantic differences before calling.
func (c *Reconciler) updateStatus(ctx context.Context, route *v1alpha1.Route) (*v1alpha1.Route, error) {
	existing, err := c.routeLister.Routes(route.Namespace).Get(route.Name)
	if err != nil {
		return nil, err
	}
	existing = existing.DeepCopy()
	// If there's nothing to update, just return.
	if reflect.DeepEqual(existing.Status, route.Status) {
		return existing, nil
	}
	existing.Status = route.Status
	// TODO: for CRD there's no updatestatus, so use normal update.
	updated, err := c.ServingClientSet.ServingV1alpha1().Routes(route.Namespace).Update(existing)
	if err != nil {
		return nil, err
	}

	c.Recorder.Eventf(route, corev1.EventTypeNormal, "Updated", "Updated status for route %q", route.Name)
	return updated, nil
}

// Update the lastPinned annotation on revisions we target so they don't get GC'd.
func (c *Reconciler) reconcileTargetRevisions(ctx context.Context, t *traffic.TrafficConfig, route *v1alpha1.Route) error {
	gcConfig := config.FromContext(ctx).GC
	lpDebounce := gcConfig.StaleRevisionLastpinnedDebounce

	eg, ctx := errgroup.WithContext(ctx)
	for _, target := range t.Targets {
		for _, rt := range target {
			tt := rt.TrafficTarget
			eg.Go(func() error {
				rev, err := c.revisionLister.Revisions(route.Namespace).Get(tt.RevisionName)
				if apierrs.IsNotFound(err) {
					c.Logger.Infof("Unable to update lastPinned for missing revision %q", tt.RevisionName)
					return nil
				} else if err != nil {
					return err
				}

				newRev := rev.DeepCopy()
				lastPin, err := newRev.GetLastPinned()
				if err != nil {
					// Missing is an expected error case for a not yet pinned revision
					if err.(v1alpha1.LastPinnedParseError).Type != v1alpha1.AnnotationParseErrorTypeMissing {
						return err
					}
				} else {
					// Enforce a delay before performing an update on lastPinned to avoid excess churn
					if lastPin.Add(lpDebounce).After(c.clock.Now()) {
						return nil
					}
				}

				if newRev.Annotations == nil {
					newRev.Annotations = make(map[string]string)
				}

				newRev.ObjectMeta.Annotations[serving.RevisionLastPinnedAnnotationKey] = v1alpha1.RevisionLastPinnedString(c.clock.Now())
				patch, err := duck.CreatePatch(rev, newRev)
				if err != nil {
					return err
				}
				patchJSON, err := json.Marshal(patch)
				if err != nil {
					return err
				}

				if _, err := c.ServingClientSet.ServingV1alpha1().Revisions(route.Namespace).Patch(rev.Name, types.MergePatchType, patchJSON); err != nil {
					c.Logger.Errorf("Unable to set revision annotation: %v", err)
					return err
				}
				return nil
			})
		}
	}
	return eg.Wait()
}
