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
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	labelerv2 "knative.dev/serving/pkg/reconciler/labeler/v2"
	"knative.dev/serving/pkg/reconciler/service/resources/names"
)

// MakeConfiguration creates a Configuration from a Service object.
func MakeConfiguration(service *v1.Service) *v1.Configuration {
	return MakeConfigurationFromExisting(service, &v1.Configuration{})
}

// MakeConfigurationFromExisting creates a Configuration from a Service object given an existing Configuration.
func MakeConfigurationFromExisting(service *v1.Service, existing *v1.Configuration) *v1.Configuration {
	labels := map[string]string{serving.ServiceLabelKey: service.Name}
	anns := kmeta.FilterMap(service.GetAnnotations(), func(key string) bool {
		return key == corev1.LastAppliedConfigAnnotation
	})

	routeName := names.Route(service)
	set := labelerv2.GetListAnnValue(existing.Annotations, serving.RoutesAnnotationKey)
	set.Insert(routeName)
	anns[serving.RoutesAnnotationKey] = strings.Join(set.UnsortedList(), ",")

	return &v1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.Configuration(service),
			Namespace: service.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*kmeta.NewControllerRef(service),
			},
			Labels:      kmeta.UnionMaps(service.GetLabels(), labels),
			Annotations: anns,
		},
		Spec: service.Spec.ConfigurationSpec,
	}
}
