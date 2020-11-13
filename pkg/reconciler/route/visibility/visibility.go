/*
Copyright 2020 The Knative Authors

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

package visibility

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apilabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	listers "k8s.io/client-go/listers/core/v1"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/network"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/domains"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"
	"knative.dev/serving/pkg/reconciler/route/traffic"
)

// Resolver resolves the visibility of traffic targets, based on both the Route and placeholder Services labels.
type Resolver struct {
	serviceLister listers.ServiceLister
}

// NewResolver returns a new Resolver.
func NewResolver(l listers.ServiceLister) *Resolver {
	return &Resolver{serviceLister: l}
}

func (b *Resolver) getServices(route *v1.Route) (map[string]*corev1.Service, error) {
	// List all the Services owned by this Route.
	currentServices, err := b.serviceLister.Services(route.Namespace).List(apilabels.SelectorFromSet(
		apilabels.Set{
			serving.RouteLabelKey: route.Name,
		},
	))
	if err != nil {
		return nil, err
	}

	serviceCopy := make(map[string]*corev1.Service, len(currentServices))
	for _, svc := range currentServices {
		serviceCopy[svc.Name] = svc.DeepCopy()
	}

	return serviceCopy, err
}

func (b *Resolver) routeVisibility(ctx context.Context, route *v1.Route) netv1alpha1.IngressVisibility {
	domainConfig := config.FromContext(ctx).Domain
	domain := domainConfig.LookupDomainForLabels(route.Labels)
	if domain == "svc."+network.GetClusterDomainName() {
		return netv1alpha1.IngressVisibilityClusterLocal
	}
	return netv1alpha1.IngressVisibilityExternalIP
}

func trafficNames(route *v1.Route) sets.String {
	names := sets.NewString(traffic.DefaultTarget)
	for _, tt := range route.Spec.Traffic {
		names.Insert(tt.Tag)
	}
	return names
}

// GetVisibility returns a map from traffic target name to their corresponding netv1alpha1.IngressVisibility.
func (b *Resolver) GetVisibility(ctx context.Context, route *v1.Route) (map[string]netv1alpha1.IngressVisibility, error) {
	// Find out the default visibility of the Route.
	defaultVisibility := b.routeVisibility(ctx, route)

	// Get all the placeholder Services to check for additional visibility settings.
	services, err := b.getServices(route)
	if err != nil {
		return nil, err
	}
	trafficNames := trafficNames(route)
	m := make(map[string]netv1alpha1.IngressVisibility, trafficNames.Len())
	for tt := range trafficNames {
		hostname, err := domains.HostnameFromTemplate(ctx, route.Name, tt)
		if err != nil {
			return nil, err
		}
		ttVisibility := netv1alpha1.IngressVisibilityExternalIP
		// Is there a visibility setting on the placeholder Service?
		if svc, ok := services[hostname]; ok {
			if labels.IsObjectLocalVisibility(&svc.ObjectMeta) {
				ttVisibility = netv1alpha1.IngressVisibilityClusterLocal
			}
		}

		// Now, choose the lowest visibility.
		m[tt] = minVisibility(ttVisibility, defaultVisibility)
	}
	return m, nil
}

func minVisibility(a, b netv1alpha1.IngressVisibility) netv1alpha1.IngressVisibility {
	if a == netv1alpha1.IngressVisibilityClusterLocal || b == netv1alpha1.IngressVisibilityClusterLocal {
		return netv1alpha1.IngressVisibilityClusterLocal
	}
	return a
}
