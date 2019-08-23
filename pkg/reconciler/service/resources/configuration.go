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
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/reconciler/service/resources/names"
	"knative.dev/serving/pkg/resources"
)

// MakeConfiguration creates a Configuration from a Service object.
func MakeConfiguration(service *v1alpha1.Service) (*v1alpha1.Configuration, error) {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.Configuration(service),
			Namespace: service.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*kmeta.NewControllerRef(service),
			},
			Labels: resources.UnionMaps(service.GetLabels(), map[string]string{
				serving.RouteLabelKey:   names.Route(service),
				serving.ServiceLabelKey: service.Name,
			}),
			Annotations: resources.FilterMap(service.GetAnnotations(), func(key string) bool {
				return key == corev1.LastAppliedConfigAnnotation
			}),
		},
		Spec: service.Spec.ConfigurationSpec,
	}, nil
}
