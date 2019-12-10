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
	"fmt"
	"reflect"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/logging"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/resources"
	"knative.dev/serving/pkg/reconciler/route/traffic"
)

func routeOwnerLabelSelector(route *v1alpha1.Route) labels.Selector {
	return labels.Set(map[string]string{
		serving.RouteLabelKey:          route.Name,
		serving.RouteNamespaceLabelKey: route.Namespace,
	}).AsSelector()
}

func (c *Reconciler) deleteIngressForRoute(route *v1alpha1.Route) error {

	// We always use DeleteCollection because even with a fixed name, we apply the labels.
	selector := routeOwnerLabelSelector(route).String()

	// Delete Ingresses owned by this route.
	return c.ServingClientSet.NetworkingV1alpha1().Ingresses(route.Namespace).DeleteCollection(
		nil, metav1.ListOptions{LabelSelector: selector})
}

func (c *Reconciler) reconcileIngress(ctx context.Context, r *v1alpha1.Route, desired *netv1alpha1.Ingress) (*netv1alpha1.Ingress, error) {
	ingress, err := c.ingressLister.Ingresses(desired.Namespace).Get(desired.Name)
	if apierrs.IsNotFound(err) {
		ingress, err = c.ServingClientSet.NetworkingV1alpha1().Ingresses(desired.Namespace).Create(desired)
		if err != nil {
			c.Recorder.Eventf(r, corev1.EventTypeWarning, "CreationFailed", "Failed to create Ingress: %v", err)
			return nil, fmt.Errorf("failed to create Ingress: %w", err)
		}

		c.Recorder.Eventf(r, corev1.EventTypeNormal, "Created", "Created Ingress %q", ingress.GetName())
		return ingress, nil
	} else if err != nil {
		return nil, err
	} else {
		// It is notable that one reason for differences here may be defaulting.
		// When that is the case, the Update will end up being a nop because the
		// webhook will bring them into alignment and no new reconciliation will occur.
		if !equality.Semantic.DeepEqual(ingress.Spec, desired.Spec) {
			// Don't modify the informers copy
			origin := ingress.DeepCopy()
			origin.Spec = desired.Spec

			updated, err := c.ServingClientSet.NetworkingV1alpha1().Ingresses(origin.Namespace).Update(origin)
			if err != nil {
				return nil, fmt.Errorf("failed to update Ingress: %w", err)
			}
			return updated, nil
		}
	}

	return ingress, err
}

func (c *Reconciler) deleteServices(namespace string, serviceNames sets.String) error {
	for _, serviceName := range serviceNames.List() {
		if err := c.KubeClientSet.CoreV1().Services(namespace).Delete(serviceName, nil); err != nil {
			return fmt.Errorf("failed to delete Service: %w", err)
		}
	}

	return nil
}

func (c *Reconciler) reconcilePlaceholderServices(ctx context.Context, route *v1alpha1.Route, targets map[string]traffic.RevisionTargets, existingServiceNames sets.String) ([]*corev1.Service, error) {
	logger := logging.FromContext(ctx)
	ns := route.Namespace

	names := sets.NewString()
	for name := range targets {
		names.Insert(name)
	}

	createdServiceNames := sets.String{}

	var services []*corev1.Service
	for _, name := range names.List() {
		desiredService, err := resources.MakeK8sPlaceholderService(ctx, route, name)
		if err != nil {
			logger.Warnw("Failed to construct placeholder k8s service", zap.Error(err))
			return nil, err
		}

		service, err := c.serviceLister.Services(ns).Get(desiredService.Name)
		if apierrs.IsNotFound(err) {
			// Doesn't exist, create it.
			service, err = c.KubeClientSet.CoreV1().Services(ns).Create(desiredService)
			if err != nil {
				c.Recorder.Eventf(route, corev1.EventTypeWarning, "CreationFailed",
					"Failed to create placeholder service %q: %v", desiredService.Name, err)
				return nil, fmt.Errorf("failed to create placeholder service: %w", err)
			}
			logger.Infof("Created service %s", desiredService.Name)
			c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created placeholder service %q", desiredService.Name)
		} else if err != nil {
			return nil, err
		} else if !metav1.IsControlledBy(service, route) {
			// Surface an error in the route's status, and return an error.
			route.Status.MarkServiceNotOwned(desiredService.Name)
			return nil, fmt.Errorf("route: %q does not own Service: %q", route.Name, desiredService.Name)
		}

		services = append(services, service)
		createdServiceNames.Insert(desiredService.Name)
	}

	// Delete any current services that was no longer desired.
	if err := c.deleteServices(ns, existingServiceNames.Difference(createdServiceNames)); err != nil {
		return nil, err
	}

	// TODO(mattmoor): This is where we'd look at the state of the Service and
	// reflect any necessary state into the Route.
	return services, nil
}

func (c *Reconciler) updatePlaceholderServices(ctx context.Context, route *v1alpha1.Route, services []*corev1.Service, ingress *netv1alpha1.Ingress) error {
	logger := logging.FromContext(ctx)
	ns := route.Namespace

	eg, _ := errgroup.WithContext(ctx)
	for _, service := range services {
		service := service
		eg.Go(func() error {
			desiredService, err := resources.MakeK8sService(ctx, route, service.Name, ingress, resources.IsClusterLocalService(service))
			if err != nil {
				// Loadbalancer not ready, no need to update.
				logger.Warnf("Failed to update k8s service: %v", err)
				return nil
			}

			// Make sure that the service has the proper specification.
			if !equality.Semantic.DeepEqual(service.Spec, desiredService.Spec) {
				// Don't modify the informers copy
				existing := service.DeepCopy()
				existing.Spec = desiredService.Spec
				_, err = c.KubeClientSet.CoreV1().Services(ns).Update(existing)
				if err != nil {
					return err
				}
			}
			return nil
		})
	}

	// TODO(mattmoor): This is where we'd look at the state of the Service and
	// reflect any necessary state into the Route.
	return eg.Wait()
}

func (c *Reconciler) updateStatus(existing *v1alpha1.Route, desired *v1alpha1.Route) error {
	existing = existing.DeepCopy()
	return reconciler.RetryUpdateConflicts(func(attempts int) (err error) {
		// The first iteration tries to use the informer's state, subsequent attempts fetch the latest state via API.
		if attempts > 0 {
			existing, err = c.ServingClientSet.ServingV1alpha1().Routes(desired.Namespace).Get(desired.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
		}

		// If there's nothing to update, just return.
		if reflect.DeepEqual(existing.Status, desired.Status) {
			return nil
		}

		existing.Status = desired.Status
		_, err = c.ServingClientSet.ServingV1alpha1().Routes(desired.Namespace).UpdateStatus(existing)
		return err
	})
}

// Update the lastPinned annotation on revisions we target so they don't get GC'd.
func (c *Reconciler) reconcileTargetRevisions(ctx context.Context, t *traffic.Config, route *v1alpha1.Route) error {
	gcConfig := config.FromContext(ctx).GC
	logger := logging.FromContext(ctx)
	lpDebounce := gcConfig.StaleRevisionLastpinnedDebounce

	eg, _ := errgroup.WithContext(ctx)
	for _, target := range t.Targets {
		for _, rt := range target {
			tt := rt.TrafficTarget
			eg.Go(func() error {
				rev, err := c.revisionLister.Revisions(route.Namespace).Get(tt.RevisionName)
				if apierrs.IsNotFound(err) {
					logger.Infof("Unable to update lastPinned for missing revision %q", tt.RevisionName)
					return nil
				} else if err != nil {
					return err
				}

				newRev := rev.DeepCopy()

				lastPin, err := newRev.GetLastPinned()
				if err != nil {
					// Missing is an expected error case for a not yet pinned revision.
					if err.(v1alpha1.LastPinnedParseError).Type != v1alpha1.AnnotationParseErrorTypeMissing {
						return err
					}
				} else {
					// Enforce a delay before performing an update on lastPinned to avoid excess churn.
					if lastPin.Add(lpDebounce).After(c.clock.Now()) {
						return nil
					}
				}

				newRev.SetLastPinned(c.clock.Now())

				patch, err := duck.CreateMergePatch(rev, newRev)
				if err != nil {
					return err
				}

				if _, err := c.ServingClientSet.ServingV1alpha1().Revisions(route.Namespace).Patch(rev.Name, types.MergePatchType, patch); err != nil {
					return fmt.Errorf("failed to set revision annotation: %w", err)
				}
				return nil
			})
		}
	}
	return eg.Wait()
}

func (c *Reconciler) reconcileCertificate(ctx context.Context, r *v1alpha1.Route, desiredCert *netv1alpha1.Certificate) (*netv1alpha1.Certificate, error) {
	cert, err := c.certificateLister.Certificates(desiredCert.Namespace).Get(desiredCert.Name)
	if apierrs.IsNotFound(err) {
		cert, err = c.ServingClientSet.NetworkingV1alpha1().Certificates(desiredCert.Namespace).Create(desiredCert)
		if err != nil {
			c.Recorder.Eventf(r, corev1.EventTypeWarning, "CreationFailed", "Failed to create Certificate: %v", err)
			return nil, fmt.Errorf("failed to create Certificate: %w", err)
		}
		c.Recorder.Eventf(r, corev1.EventTypeNormal, "Created",
			"Created Certificate %s/%s", cert.Namespace, cert.Name)
		return cert, nil
	} else if err != nil {
		return nil, err
	} else if !metav1.IsControlledBy(cert, r) {
		// Surface an error in the route's status, and return an error.
		r.Status.MarkCertificateNotOwned(cert.Name)
		return nil, fmt.Errorf("route: %s does not own certificate: %s", r.Name, cert.Name)
	} else {
		if !equality.Semantic.DeepEqual(cert.Spec, desiredCert.Spec) {
			// Don't modify the informers copy
			existing := cert.DeepCopy()
			existing.Spec = desiredCert.Spec
			cert, err := c.ServingClientSet.NetworkingV1alpha1().Certificates(existing.Namespace).Update(existing)
			if err != nil {
				c.Recorder.Eventf(r, corev1.EventTypeWarning, "UpdateFailed",
					"Failed to update Certificate %s/%s: %v", existing.Namespace, existing.Name, err)
				return nil, err
			}
			c.Recorder.Eventf(existing, corev1.EventTypeNormal, "Updated",
				"Updated Spec for Certificate %s/%s", existing.Namespace, existing.Name)
			return cert, nil
		}
	}
	return cert, nil
}
