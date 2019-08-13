/*
Copyright 2019 The Knative Authors

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

package route

import (
	"context"
	"fmt"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/pkg/logging"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	networkinglisters "knative.dev/serving/pkg/client/private/listers/networking/v1alpha1"
	"knative.dev/serving/pkg/reconciler/route/resources"
	resourcenames "knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/pkg/reconciler/route/traffic"
)

// ClusterIngressResources Cluster Ingress resources
type ClusterIngressResources struct {
	BaseIngressResources
	clusterIngressLister networkinglisters.ClusterIngressLister
}

// makeIngress constructs a new ClusterIngress object
func (cir *ClusterIngressResources) makeIngress(
	ctx context.Context,
	r *v1alpha1.Route,
	tc *traffic.Config,
	tls []netv1alpha1.IngressTLS,
	clusterLocalServices sets.String,
	ingressClass string) (netv1alpha1.IngressAccessor, error) {

	logger := logging.FromContext(ctx)
	logger.Info("Creating ClusterIngress.")

	return resources.MakeClusterIngress(ctx, r, tc, tls, clusterLocalServices, ingressClass)
}

// createIngress invokes APIs to create a new ClusterIngress
func (cir *ClusterIngressResources) createIngress(desired netv1alpha1.IngressAccessor) (netv1alpha1.IngressAccessor, error) {
	return cir.privateClientSet.NetworkingV1alpha1().ClusterIngresses().Create(desired.(*netv1alpha1.ClusterIngress))
}

// getIngressForRoute retrieves a ClusterIngress from a given route
func (cir *ClusterIngressResources) getIngressForRoute(route *v1alpha1.Route) (netv1alpha1.IngressAccessor, error) {
	// First, look up the fixed name.
	ciName := resourcenames.ClusterIngress(route)
	ci, err := cir.clusterIngressLister.Get(ciName)
	if err == nil {
		return ci, nil
	}

	// If that isn't found, then fallback on the legacy selector-based approach.
	selector := routeOwnerLabelSelector(route)
	ingresses, err := cir.clusterIngressLister.List(selector)
	if err != nil {
		return nil, err
	}
	if len(ingresses) == 0 {
		return nil, apierrs.NewNotFound(
			v1alpha1.Resource("clusteringress"), resourcenames.ClusterIngress(route))
	}

	if len(ingresses) > 1 {
		// Return error as we expect only one ingress instance for a route.
		return nil, fmt.Errorf("more than one ClusterIngress are found for route %s/%s: %v", route.Namespace, route.Name, ingresses)
	}

	return ingresses[0], nil
}

// updateIngress invokes APIs to update a ClusterIngress
func (cir *ClusterIngressResources) updateIngress(origin netv1alpha1.IngressAccessor) (netv1alpha1.IngressAccessor, error) {
	return cir.privateClientSet.NetworkingV1alpha1().ClusterIngresses().Update(origin.(*netv1alpha1.ClusterIngress))
}
