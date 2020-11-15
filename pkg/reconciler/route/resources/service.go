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
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"

	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/domains"
)

// GetNames returns a set of service names.
func GetNames(services []*corev1.Service) sets.String {
	names := make(sets.String, len(services))

	for i := range services {
		names.Insert(services[i].Name)
	}

	return names
}

// SelectorFromRoute creates a label selector given a specific route.
func SelectorFromRoute(route *v1.Route) labels.Selector {
	return labels.SelectorFromSet(labels.Set{serving.RouteLabelKey: route.Name})
}

// MakeK8sPlaceholderService creates a placeholder Service to prevent naming collisions. It's owned by the
// provided v1.Route.
func MakeK8sPlaceholderService(ctx context.Context, route *v1.Route, targetName string) (*corev1.Service, error) {
	service, err := makeK8sService(ctx, route, targetName)
	if err != nil {
		return nil, err
	}
	service.Spec = corev1.ServiceSpec{
		Type:            corev1.ServiceTypeClusterIP,
		SessionAffinity: corev1.ServiceAffinityNone,
		Ports:           defaultServicePort,
	}

	return service, nil
}

var defaultServicePort = []corev1.ServicePort{{
	Name: networking.ServicePortNameHTTP1,
	Port: networking.ServiceHTTPPort,
}}

func makeK8sService(ctx context.Context, route *v1.Route, targetName string) (*corev1.Service, error) {
	hostname, err := domains.HostnameFromTemplate(ctx, route.Name, targetName)
	if err != nil {
		return nil, err
	}

	svcLabels := map[string]string{
		serving.RouteLabelKey: route.Name,
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hostname,
			Namespace: route.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				// This service is owned by the Route.
				*kmeta.NewControllerRef(route),
			},
			Labels: kmeta.UnionMaps(kmeta.FilterMap(route.GetLabels(), func(key string) bool {
				// Do not propagate the visibility label from Route as users may want to set the label
				// in the specific k8s svc for subroute. see https://github.com/knative/serving/pull/4560.
				return (key == network.VisibilityLabelKey || key == serving.VisibilityLabelKeyObsolete)
			}), svcLabels),
			Annotations: route.GetAnnotations(),
		},
	}, nil
}

// MakeEndpoints constructs a K8s Endpoints by copying the subsets from KIngress's endpoints.
func MakeEndpoints(ctx context.Context, svc *corev1.Service, route *v1.Route, src *corev1.Endpoints) *corev1.Endpoints {
	svcLabels := map[string]string{
		serving.RouteLabelKey: route.Name,
	}
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:        svc.Name, // Name of Endpoints must match that of Service.
			Namespace:   svc.Namespace,
			Labels:      kmeta.UnionMaps(route.GetLabels(), svcLabels),
			Annotations: route.GetAnnotations(),
			OwnerReferences: []metav1.OwnerReference{
				// This service is owned by the Route.
				*kmeta.NewControllerRef(route),
			},
		},
		Subsets: src.Subsets,
	}
}
