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
	"github.com/knative/serving/pkg/controller/route/istio"
	"github.com/knative/serving/pkg/logging"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

/////////////////////////////////////////
// Helper functions for CREATE/UPDATE operations of various resources.
/////////////////////////////////////////
func (c *Controller) reconcileVirtualService(ctx context.Context, route *v1alpha1.Route, vs *v1alpha3.VirtualService) error {
	logger := logging.FromContext(ctx)
	vsClient := c.ServingClientSet.NetworkingV1alpha3().VirtualServices(vs.Namespace)
	existing, err := vsClient.Get(vs.Name, metav1.GetOptions{})
	if err == nil && !reflect.DeepEqual(existing.Spec, vs.Spec) {
		existing.Spec = vs.Spec
		_, err = vsClient.Update(existing)
		logger.Error("Failed to update VirtualService", zap.Error(err))
		return err
	} else if apierrs.IsNotFound(err) {
		_, err = vsClient.Create(vs)
		if err != nil {
			logger.Error("Failed to create VirtualService", zap.Error(err))
		} else {
			c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created VirtualService %q", vs.Name)
		}
		return err
	}
	return err
}

func (c *Controller) reconcilePlaceholderService(ctx context.Context, route *v1alpha1.Route) error {
	logger := logging.FromContext(ctx)
	service := istio.MakeRouteK8SService(route)
	if _, err := c.KubeClientSet.CoreV1().Services(route.Namespace).Create(service); err != nil {
		if apierrs.IsAlreadyExists(err) {
			// Service already exist.
			return nil
		}
		logger.Error("Failed to create service", zap.Error(err))
		c.Recorder.Eventf(route, corev1.EventTypeWarning, "CreationFailed", "Failed to create service %q: %v", service.Name, err)
		return err
	}
	logger.Infof("Created service %s", service.Name)
	c.Recorder.Eventf(route, corev1.EventTypeNormal, "Created", "Created service %q", service.Name)
	return nil
}

// Update the Status of the route.  Caller is responsible for checking
// for semantic differences before calling.
func (c *Controller) updateStatus(ctx context.Context, route *v1alpha1.Route) (*v1alpha1.Route, error) {
	logger := logging.FromContext(ctx)

	routeClient := c.ServingClientSet.ServingV1alpha1().Routes(route.Namespace)
	existing, err := routeClient.Get(route.Name, metav1.GetOptions{})
	if err != nil {
		logger.Warn("Failed to update route status", zap.Error(err))
		c.Recorder.Eventf(route, corev1.EventTypeWarning, "UpdateFailed", "Failed to get current status for route %q: %v", route.Name, err)
		return nil, err
	}

	existing.Status = route.Status
	updated, err := routeClient.Update(existing)
	if err != nil {
		logger.Warn("Failed to update route status", zap.Error(err))
		c.Recorder.Eventf(route, corev1.EventTypeWarning, "UpdateFailed", "Failed to update status for route %q: %v", route.Name, err)
		return nil, err
	}

	c.Recorder.Eventf(route, corev1.EventTypeNormal, "Updated", "Updated status for route %q", route.Name)
	return updated, nil
}
