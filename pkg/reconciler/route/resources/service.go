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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/networking"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/route/domains"
)

var errLoadBalancerNotFound = errors.New("failed to fetch loadbalancer domain/IP from ingress status")

// SelectorFromRoute creates a label selector given a specific route.
func SelectorFromRoute(route *v1alpha1.Route) labels.Selector {
	return labels.SelectorFromSet(
		labels.Set{
			serving.RouteLabelKey: route.Name,
		},
	)
}

// MakeK8sPlaceholderService creates a placeholder Service to prevent naming collisions. It's owned by the
// provided v1alpha1.Route. The purpose of this service is to provide a placeholder domain name for Istio routing.
func MakeK8sPlaceholderService(ctx context.Context, route *v1alpha1.Route, targetName string) (*corev1.Service, error) {
	name := domains.SubdomainName(route, targetName)
	fullName, err := domains.DomainNameFromTemplate(ctx, route, name)
	if err != nil {
		return nil, err
	}

	service := makeK8sService(route, targetName)
	service.Spec = corev1.ServiceSpec{
		Type:         corev1.ServiceTypeExternalName,
		ExternalName: fullName,
	}

	return service, nil
}

// MakeK8sService creates a Service that redirect to the loadbalancer specified
// in ClusterIngress status. It's owned by the provided v1alpha1.Route.
// The purpose of this service is to provide a domain name for Istio routing.
func MakeK8sService(route *v1alpha1.Route, targetName string, ingress *netv1alpha1.ClusterIngress) (*corev1.Service, error) {
	svcSpec, err := makeServiceSpec(ingress)
	if err != nil {
		return nil, err
	}

	service := makeK8sService(route, targetName)
	service.Spec = *svcSpec
	return service, nil
}

func makeK8sService(route *v1alpha1.Route, targetName string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      domains.SubdomainName(route, targetName),
			Namespace: route.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				// This service is owned by the Route.
				*kmeta.NewControllerRef(route),
			},
			Labels: map[string]string{
				serving.RouteLabelKey: route.Name,
			},
		},
	}
}

func makeServiceSpec(ingress *netv1alpha1.ClusterIngress) (*corev1.ServiceSpec, error) {
	ingressStatus := ingress.Status
	if ingressStatus.LoadBalancer == nil || len(ingressStatus.LoadBalancer.Ingress) == 0 {
		return nil, errLoadBalancerNotFound
	}
	if len(ingressStatus.LoadBalancer.Ingress) > 1 {
		// Return error as we only support one LoadBalancer currently.
		return nil, fmt.Errorf("more than one ingress are specified in status(LoadBalancer) of ClusterIngress %s", ingress.Name)
	}
	balancer := ingressStatus.LoadBalancer.Ingress[0]

	// Here we decide LoadBalancer information in the order of
	// DomainInternal > Domain > LoadBalancedIP to prioritize cluster-local,
	// and domain (since it would change less than IP).
	switch {
	case len(balancer.DomainInternal) != 0:
		return &corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: balancer.DomainInternal,
		}, nil
	case len(balancer.Domain) != 0:
		return &corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: balancer.Domain,
		}, nil
	case balancer.MeshOnly:
		// The ClusterIngress is loadbalanced through a Service mesh.
		// We won't have a specific LB endpoint to route traffic to,
		// but we still need to create a ClusterIP service to make
		// sure the domain name is available for access within the
		// mesh.
		return &corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{
				Name: networking.ServicePortNameHTTP1,
				Port: networking.ServiceHTTPPort,
			}},
		}, nil
	case len(balancer.IP) != 0:
		// TODO(lichuqiang): deal with LoadBalancer IP.
		// We'll also need ports info to make it take effect.
	}
	return nil, errLoadBalancerNotFound
}
