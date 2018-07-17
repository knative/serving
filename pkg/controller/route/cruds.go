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
	"reflect"

	"github.com/knative/serving/pkg/apis/istio/v1alpha3"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller/route/resources"
	resourcenames "github.com/knative/serving/pkg/controller/route/resources/names"
	"github.com/knative/serving/pkg/logging"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
)

func (c *Controller) reconcileVirtualService(ctx context.Context, route *v1alpha1.Route,
	desiredVirtualService *v1alpha3.VirtualService) error {
	logger := logging.FromContext(ctx)
	ns := desiredVirtualService.Namespace
	name := desiredVirtualService.Name

	virtualService, err := c.virtualServiceLister.VirtualServices(ns).Get(name)
	if apierrs.IsNotFound(err) {
		virtualService, err = c.ServingClientSet.NetworkingV1alpha3().VirtualServices(ns).Create(desiredVirtualService)
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
		virtualService.Spec = desiredVirtualService.Spec
		virtualService, err = c.ServingClientSet.NetworkingV1alpha3().VirtualServices(ns).Update(virtualService)
		if err != nil {
			logger.Error("Failed to update VirtualService", zap.Error(err))
			return err
		}
	}

	// TODO(mattmoor): This is where we'd look at the state of the VirtualService and
	// reflect any necessary state into the Route.
	return err
}

func (c *Controller) reconcilePlaceholderService(ctx context.Context, route *v1alpha1.Route) error {
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
		route.Status.DomainInternal = resourcenames.K8sServiceFullname(route)
		c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created service %q", name)
	} else if err != nil {
		return err
	} else {
		// Make sure that the service has the proper specification
		desiredService := resources.MakeK8sService(route)
		// Preserve the ClusterIP field in the Service's Spec, if it has been set.
		desiredService.Spec.ClusterIP = service.Spec.ClusterIP
		if !equality.Semantic.DeepEqual(service.Spec, desiredService.Spec) {
			service.Spec = desiredService.Spec
			service, err = c.KubeClientSet.CoreV1().Services(ns).Update(service)
			if err != nil {
				return err
			}
		}
	}

	// TODO(mattmoor): This is where we'd look at the state of the Service and
	// reflect any necessary state into the Route.
	return nil
}

// Update the Status of the route.  Caller is responsible for checking
// for semantic differences before calling.
func (c *Controller) updateStatus(ctx context.Context, route *v1alpha1.Route) (*v1alpha1.Route, error) {
	existing, err := c.routeLister.Routes(route.Namespace).Get(route.Name)
	if err != nil {
		return nil, err
	}
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
