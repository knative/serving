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
	"log"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

func GetElaConfigMapName() string {
	return "ela-config"
}

// Various functions for naming the resources for consistency
func GetElaNamespaceName(ns string) string {
	// We create resources in the same namespace as the Elafros resources by default.
	// TODO(mattmoor): Expose a knob for creating resources in an alternate namespace.
	return ns
}

func GetRevisionDeploymentName(u *v1alpha1.Revision) string {
	return u.Name + "-deployment"
}

func GetRevisionAutoscalerName(u *v1alpha1.Revision) string {
	return u.Name + "-autoscaler"
}

func GetElaIstioRouteRuleName(u *v1alpha1.Route) string {
	return u.Name + "-istio"
}

func GetElaK8SIngressName(u *v1alpha1.Route) string {
	return u.Name + "-ela-ingress"
}

func GetElaK8SServiceNameForRevision(u *v1alpha1.Revision) string {
	return u.Name + "-service"
}

func GetElaK8SServiceName(u *v1alpha1.Route) string {
	return u.Name + "-service"
}

func GetElaK8SRouterServiceName(u *v1alpha1.Route) string {
	return "router-service"
}

func GetOrCreateRevisionNamespace(ns string, c clientset.Interface) (string, error) {
	return GetOrCreateNamespace(GetElaNamespaceName(ns), c)
}

func GetOrCreateNamespace(namespace string, c clientset.Interface) (string, error) {
	_, err := c.Core().Namespaces().Get(namespace, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			log.Printf("namespace: %v, unable to get namespace due to error: %v", namespace, err)
			return "", err
		}
		log.Printf("namespace: %v, not found. Creating...", namespace)
		nsObj := &corev1.Namespace{
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
