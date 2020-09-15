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
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"

	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/domains"
)

var errLoadBalancerNotFound = errors.New("failed to fetch loadbalancer domain/IP from ingress status")

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
// provided v1.Route. The purpose of this service is to provide a placeholder domain name for Istio routing.
func MakeK8sPlaceholderService(ctx context.Context, route *v1.Route, targetName string) (*corev1.Service, error) {
	hostname, err := domains.HostnameFromTemplate(ctx, route.Name, targetName)
	if err != nil {
		return nil, err
	}
	fullName, err := domains.DomainNameFromTemplate(ctx, route.ObjectMeta, hostname)
	if err != nil {
		return nil, err
	}

	service, err := makeK8sService(ctx, route, targetName)
	if err != nil {
		return nil, err
	}
	service.Spec = corev1.ServiceSpec{
		Type:            corev1.ServiceTypeExternalName,
		ExternalName:    fullName,
		SessionAffinity: corev1.ServiceAffinityNone,
		Ports: []corev1.ServicePort{{
			Name:       networking.ServicePortNameH2C,
			Port:       int32(80),
			TargetPort: intstr.FromInt(80),
		}},
	}

	return service, nil
}

// MakeK8sService creates a Service that redirect to the loadbalancer specified
// in Ingress status. It's owned by the provided v1.Route.
// The purpose of this service is to provide a domain name for Istio routing.
func MakeK8sService(ctx context.Context, route *v1.Route, targetName string, ingress *netv1alpha1.Ingress, isPrivate bool, clusterIP string) (*corev1.Service, error) {
	svcSpec, err := makeServiceSpec(ingress, isPrivate, clusterIP)
	if err != nil {
		return nil, err
	}

	service, err := makeK8sService(ctx, route, targetName)
	if err != nil {
		return nil, err
	}
	service.Spec = *svcSpec
	return service, nil
}

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

func makeServiceSpec(ingress *netv1alpha1.Ingress, isPrivate bool, clusterIP string) (*corev1.ServiceSpec, error) {
	ingressStatus := ingress.Status

	lbStatus := ingressStatus.PublicLoadBalancer
	if isPrivate || ingressStatus.PrivateLoadBalancer != nil {
		// Always use private load balancer if it exists,
		// because k8s service is only useful for inter-cluster communication.
		// External communication will be handle via ingress gateway, which won't be affected by what is configured here.
		lbStatus = ingressStatus.PrivateLoadBalancer
	}

	if lbStatus == nil || len(lbStatus.Ingress) == 0 {
		return nil, errLoadBalancerNotFound
	}
	if len(lbStatus.Ingress) > 1 {
		// Return error as we only support one LoadBalancer currently.
		return nil, errors.New(
			"more than one ingress are specified in status(LoadBalancer) of Ingress " + ingress.GetName())
	}
	balancer := lbStatus.Ingress[0]

	// Here we decide LoadBalancer information in the order of
	// DomainInternal > Domain > LoadBalancedIP to prioritize cluster-local,
	// and domain (since it would change less than IP).
	switch {
	case balancer.DomainInternal != "":
		return &corev1.ServiceSpec{
			Type:            corev1.ServiceTypeExternalName,
			ExternalName:    balancer.DomainInternal,
			SessionAffinity: corev1.ServiceAffinityNone,
			Ports: []corev1.ServicePort{{
				Name:       networking.ServicePortNameH2C,
				Port:       int32(80),
				TargetPort: intstr.FromInt(80),
			}},
		}, nil
	case balancer.Domain != "":
		return &corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: balancer.Domain,
			Ports: []corev1.ServicePort{{
				Name:       networking.ServicePortNameH2C,
				Port:       int32(80),
				TargetPort: intstr.FromInt(80),
			}},
		}, nil
	case balancer.MeshOnly:
		// The Ingress is loadbalanced through a Service mesh.
		// We won't have a specific LB endpoint to route traffic to,
		// but we still need to create a ClusterIP service to make
		// sure the domain name is available for access within the
		// mesh.
		return &corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: clusterIP,
			Ports: []corev1.ServicePort{{
				Name: networking.ServicePortNameHTTP1,
				Port: networking.ServiceHTTPPort,
			}},
		}, nil
	case balancer.IP != "":
		// TODO(lichuqiang): deal with LoadBalancer IP.
		// We'll also need ports info to make it take effect.
	}
	return nil, errLoadBalancerNotFound
}

// GetDesiredServiceNames returns a list of service names that we expect to create
func GetDesiredServiceNames(ctx context.Context, route *v1.Route) (sets.String, error) {
	traffic := route.Spec.Traffic

	// We always want create the route with the service name.
	// If the traffic stanza only contains revision targets, then
	// this will not be added below, and as a consequence we'll create
	// a public route to it.
	names := sets.NewString(route.Name)

	for _, t := range traffic {
		serviceName, err := domains.HostnameFromTemplate(ctx, route.Name, t.Tag)
		if err != nil {
			return nil, err
		}
		names.Insert(serviceName)
	}

	return names, nil
}
