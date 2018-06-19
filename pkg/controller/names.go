/*
Copyright 2018 Google LLC

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

package controller

import (
	"context"

	"go.uber.org/zap"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/logging"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

func GetDomainConfigMapName() string {
	return "config-domain"
}

func GetNetworkConfigMapName() string {
	return "config-network"
}

// Various functions for naming the resources for consistency
func GetServingNamespaceName(ns string) string {
	// We create resources in the same namespace as the Knative Serving resources by default.
	// TODO(mattmoor): Expose a knob for creating resources in an alternate namespace.
	return ns
}

func GetRevisionDeploymentName(u *v1alpha1.Revision) string {
	return u.Name + "-deployment"
}

func GetRevisionAutoscalerName(u *v1alpha1.Revision) string {
	return u.Name + "-autoscaler"
}

func GetRouteRuleName(u *v1alpha1.Route, tt *v1alpha1.TrafficTarget) string {
	if tt != nil {
		return u.Name + "-" + tt.Name + "-istio"
	}
	return u.Name + "-istio"
}

func GetServingK8SIngressName(u *v1alpha1.Route) string {
	return u.Name + "-ingress"
}

func GetServingK8SServiceNameForRevision(u *v1alpha1.Revision) string {
	return u.Name + "-service"
}

func GetServingK8SServiceName(u *v1alpha1.Route) string {
	return u.Name + "-service"
}

func GetServiceConfigurationName(u *v1alpha1.Service) string {
	return u.Name
}

func GetServiceRouteName(u *v1alpha1.Service) string {
	return u.Name
}

func GetServingK8SActivatorServiceName() string {
	return "activator-service"
}

func GetRevisionHeaderName() string {
	return "Knative-Serving-Revision"
}

func GetRevisionHeaderNamespace() string {
	return "Knative-Serving-Namespace"
}

func GetOrCreateRevisionNamespace(ctx context.Context, ns string, c clientset.Interface) (string, error) {
	return GetOrCreateNamespace(ctx, GetServingNamespaceName(ns), c)
}

func GetOrCreateNamespace(ctx context.Context, namespace string, c clientset.Interface) (string, error) {
	_, err := c.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
	if err != nil {
		logger := logging.FromContext(ctx)
		if !apierrs.IsNotFound(err) {
			logger.Errorf("namespace: %v, unable to get namespace due to error: %v", namespace, err)
			return "", err
		}
		logger.Infof("namespace: %v, not found. Creating...", namespace)
		nsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespace,
				Namespace: "",
			},
		}
		_, err := c.CoreV1().Namespaces().Create(nsObj)
		if err != nil {
			logger.Error("Unexpected error while creating namespace", zap.Error(err))
			return "", err
		}
	}
	return namespace, nil
}
