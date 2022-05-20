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
	"k8s.io/apimachinery/pkg/util/intstr"

	netapi "knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/domains"
)

type ServicePair struct {
	*corev1.Service
	*corev1.Endpoints
	Tag string
}

var errLoadBalancerNotFound = errors.New("failed to fetch loadbalancer domain/IP from ingress status")

// MakeK8sPlaceholderService creates a placeholder Service to prevent naming collisions.
// It's owned by the provided v1.Route.
func MakeK8sPlaceholderService(ctx context.Context, route *v1.Route, tagName string) (*corev1.Service, error) {
	hostname, err := domains.HostnameFromTemplate(ctx, route.Name, tagName)
	if err != nil {
		return nil, err
	}

	domainName, err := domains.DomainNameFromTemplate(ctx, route.ObjectMeta, hostname)
	if err != nil {
		return nil, err
	}

	return &corev1.Service{
		ObjectMeta: makeServiceObjectMeta(hostname, route),
		Spec: corev1.ServiceSpec{
			Type:            corev1.ServiceTypeExternalName,
			ExternalName:    domainName,
			SessionAffinity: corev1.ServiceAffinityNone,
			Ports: []corev1.ServicePort{{
				Name:       netapi.ServicePortNameH2C,
				Port:       int32(80),
				TargetPort: intstr.FromInt(80),
			}},
		},
	}, nil
}

// MakeK8sService creates a Service that redirect to the loadbalancer specified
// in Ingress status. It's owned by the provided v1.Route.
func MakeK8sService(ctx context.Context, route *v1.Route, tagName string, ingress *netv1alpha1.Ingress, isPrivate bool) (*ServicePair, error) {
	privateLB := ingress.Status.PrivateLoadBalancer
	lbStatus := ingress.Status.PublicLoadBalancer

	if isPrivate || privateLB != nil && len(privateLB.Ingress) != 0 {
		// Always use private load balancer if it exists,
		// because k8s service is only useful for inter-cluster communication.
		// External communication will be handle via ingress gateway, which won't be affected by what is configured here.
		lbStatus = privateLB
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

	hostname, err := domains.HostnameFromTemplate(ctx, route.Name, tagName)
	if err != nil {
		return nil, err
	}

	pair := &ServicePair{
		Service: &corev1.Service{
			ObjectMeta: makeServiceObjectMeta(hostname, route),
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
					Name:       netapi.ServicePortNameH2C,
					Port:       int32(80),
					TargetPort: intstr.FromInt(80),
				}},
			},
		},
		Endpoints: nil,
	}

	// We want to avoid ExternalName K8s Services for the reasons outlined
	// here: https://github.com/knative/serving/issues/11821
	//
	// Thus we prioritize IP > DomainInternal > Domain
	//
	// Once we can switch to EndpointSlices (1.22) we can hopefully deprioritize
	// IP to where it was before
	switch {
	case balancer.IP != "":
		pair.Service.Spec.ClusterIP = corev1.ClusterIPNone
		pair.Service.Spec.Type = corev1.ServiceTypeClusterIP
		pair.Endpoints = &corev1.Endpoints{
			ObjectMeta: pair.Service.ObjectMeta,
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: balancer.IP,
				}},
				Ports: []corev1.EndpointPort{{
					Name: netapi.ServicePortNameH2C,
					Port: int32(80),
				}},
			}},
		}
	case balancer.DomainInternal != "":
		pair.Service.Spec.Type = corev1.ServiceTypeExternalName
		pair.Service.Spec.ExternalName = balancer.DomainInternal
		pair.Service.Spec.SessionAffinity = corev1.ServiceAffinityNone
	case balancer.Domain != "":
		pair.Service.Spec.Type = corev1.ServiceTypeExternalName
		pair.Service.Spec.ExternalName = balancer.Domain
	case balancer.MeshOnly:
		// The Ingress is loadbalanced through a Service mesh.
		// We won't have a specific LB endpoint to route traffic to,
		// but we still need to create a ClusterIP service to make
		// sure the domain name is available for access within the
		// mesh.
		pair.Service.Spec.Type = corev1.ServiceTypeClusterIP
		pair.Service.Spec.Ports = []corev1.ServicePort{{
			Name: netapi.ServicePortNameHTTP1,
			Port: netapi.ServiceHTTPPort,
		}}
	default:
		return nil, errLoadBalancerNotFound
	}
	return pair, nil
}

func makeServiceObjectMeta(hostname string, route *v1.Route) metav1.ObjectMeta {
	svcLabels := map[string]string{
		serving.RouteLabelKey: route.Name,
	}

	return metav1.ObjectMeta{
		Name:      hostname,
		Namespace: route.Namespace,
		OwnerReferences: []metav1.OwnerReference{
			// This service is owned by the Route.
			*kmeta.NewControllerRef(route),
		},
		Labels: kmeta.UnionMaps(kmeta.FilterMap(route.GetLabels(), func(key string) bool {
			// Do not propagate the visibility label from Route as users may want to set the label
			// in the specific k8s svc for subroute. see https://github.com/knative/serving/pull/4560.
			return key == netapi.VisibilityLabelKey
		}), svcLabels),
		Annotations: route.GetAnnotations(),
	}
}
