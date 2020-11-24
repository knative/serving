/*
Copyright 2018 The Knative Authors

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

package resources

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/service/resources/names"
)

// MakeRoute creates a Route from a Service object.
func MakeRoute(service *v1.Service) *v1.Route {
	c := &v1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.Route(service),
			Namespace: service.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*kmeta.NewControllerRef(service),
			},
			Annotations: kmeta.FilterMap(service.GetAnnotations(), func(key string) bool {
				return key == corev1.LastAppliedConfigAnnotation
			}),
			Labels: kmeta.UnionMaps(service.GetLabels(), map[string]string{
				// Add this service's name to the route annotations.
				serving.ServiceLabelKey: service.Name,
			}),
		},
		Spec: *service.Spec.RouteSpec.DeepCopy(),
	}

	// Fill in any missing ConfigurationName fields when translating
	// from Service to Route.
	for idx := range c.Spec.Traffic {
		if c.Spec.Traffic[idx].RevisionName == "" {
			c.Spec.Traffic[idx].ConfigurationName = names.Configuration(service)
		}
	}

	return c
}
