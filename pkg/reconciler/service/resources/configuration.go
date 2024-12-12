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
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/kmap"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/labeler"
	"knative.dev/serving/pkg/reconciler/service/resources/names"
)

// MakeConfiguration creates a Configuration from a Service object.
func MakeConfiguration(service *v1.Service) *v1.Configuration {
	return MakeConfigurationFromExisting(service, &v1.Configuration{})
}

// MakeConfigurationFromExisting creates a Configuration from a Service object given an existing Configuration.
func MakeConfigurationFromExisting(service *v1.Service, existing *v1.Configuration) *v1.Configuration {
	labels := map[string]string{
		serving.ServiceLabelKey:    service.Name,
		serving.ServiceUIDLabelKey: string(service.ObjectMeta.UID),
	}

	exclude := append([]string{corev1.LastAppliedConfigAnnotation}, serving.RolloutDurationAnnotation...)
	anns := kmap.ExcludeKeyList(service.GetAnnotations(), exclude)

	routeName := names.Route(service)
	set := labeler.GetListAnnValue(existing.Annotations, serving.RoutesAnnotationKey)
	set.Insert(routeName)
	routeNames := set.UnsortedList()
	sort.Strings(routeNames)
	anns[serving.RoutesAnnotationKey] = strings.Join(routeNames, ",")

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
