/*
Copyright 2017 The Kubernetes Authors.

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

package util

import (
	"fmt"
	"log"

	"github.com/google/elafros/pkg/apis/ela/v1alpha1"

	"github.com/google/uuid"
	apiv1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// Various functions for naming the resources for consistency
func GetElaNamespaceName(ns string) string {
	return ns + "-ela"
}

func GetRevisionDeploymentName(u *v1alpha1.Revision) string {
	return u.Name + "-" + u.Spec.Service + "-ela-deployment"
}

func GetRevisionNginxConfigMapName(u *v1alpha1.Revision) string {
	return u.Name + "-" + u.Spec.Service + "-proxy-configmap"
}

func GetRevisionAutoscalerName(u *v1alpha1.Revision) string {
	return u.Name + "-" + u.Spec.Service + "-autoscaler"
}

func GetRevisionPodName(u *v1alpha1.Revision) string {
	genUUID, err := uuid.NewRandom()
	if err != nil {
		return fmt.Sprintf("%s-warmpod-uuid-failed", u.Name)
	}
	return fmt.Sprintf("%s-warmpod-%s", u.Name, genUUID)
}

func GetElaIstioRouteRuleName(u *v1alpha1.ElaService) string {
	return u.Name + "-istio"
}

func GetElaK8SIngressName(u *v1alpha1.ElaService) string {
	return u.Name + "-ela-ingress"
}

func GetElaK8SServiceNameForRevision(u *v1alpha1.Revision) string {
	return u.Name + "-service"
}

func GetElaK8SServiceName(u *v1alpha1.ElaService) string {
	return u.Name + "-service"
}

func GetElaK8SRouterServiceName(u *v1alpha1.ElaService) string {
	return "router-service"
}

func GetOrCreateAppDeploymentNamespace(resourceNamespace string, c clientset.Interface) (string, error) {
	return GetOrCreateNamespace(resourceNamespace+"-app", c)
}

func GetOrCreateElaDeploymentNamespace(resourceNamespace string, c clientset.Interface) (string, error) {
	return GetOrCreateNamespace(GetElaNamespaceName(resourceNamespace), c)
}

func GetOrCreateRevisionNamespace(resourceNamespace string, c clientset.Interface) (string, error) {
	return GetOrCreateNamespace(GetElaNamespaceName(resourceNamespace), c)
}

func GetOrCreateNamespace(namespace string, c clientset.Interface) (string, error) {
	_, err := c.Core().Namespaces().Get(namespace, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			log.Printf("namespace: %v, unable to get namespace due to error: %v", namespace, err)
			return "", err
		}
		log.Printf("namespace: %v, not found. Creating...", namespace)
		nsObj := &apiv1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespace,
				Namespace: "",
			},
		}
		_, err := c.Core().Namespaces().Create(nsObj)
		if err != nil {
			log.Printf("Unexpected error while creating namespace: %v", err)
			return "", err
		}
	}
	return namespace, nil
}
