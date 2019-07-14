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

	"k8s.io/apimachinery/pkg/util/sets"

	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	networkinglisters "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/route/resources"
	resourcenames "github.com/knative/serving/pkg/reconciler/route/resources/names"
	"github.com/knative/serving/pkg/reconciler/route/traffic"
	"knative.dev/pkg/logging"
)

// IngressResourceAccessors Interface for accessing ingress resource
type IngressResourceAccessors interface {

	// makeIngress constructs a new ingress object
	makeIngress(ctx context.Context, r *v1alpha1.Route, tc *traffic.Config, tls []netv1alpha1.IngressTLS,
		clusterLocalServices sets.String, ingressClass string) (netv1alpha1.IngressAccessor, error)

	// createIngress creates a new ingress in a cluster
	createIngress(desired netv1alpha1.IngressAccessor) (netv1alpha1.IngressAccessor, error)

	// getIngressForRoute retrieves an ingress for a given route
	getIngressForRoute(route *v1alpha1.Route) (netv1alpha1.IngressAccessor, error)

	// updateIngress updates an ingress in a cluster
	updateIngress(origin netv1alpha1.IngressAccessor) (netv1alpha1.IngressAccessor, error)
}

// BaseIngressResources Common base struct for ingress resource
type BaseIngressResources struct {
	servingClientSet clientset.Interface
}

// IngressResources Namespaced ingress resources
type IngressResources struct {
	BaseIngressResources
	ingressLister networkinglisters.IngressLister
}

// makeIngress constructs a new Ingress object
func (ir *IngressResources) makeIngress(
	ctx context.Context,
	r *v1alpha1.Route,
	tc *traffic.Config,
	tls []netv1alpha1.IngressTLS,
	clusterLocalServices sets.String,
	ingressClass string) (netv1alpha1.IngressAccessor, error) {

	logger := logging.FromContext(ctx)
	logger.Info("Creating Ingress.")

	return resources.MakeIngress(ctx, r, tc, tls, clusterLocalServices, ingressClass)
}

// createIngress invokes APIs to create a new Ingress
func (ir *IngressResources) createIngress(desired netv1alpha1.IngressAccessor) (netv1alpha1.IngressAccessor, error) {
	return ir.servingClientSet.NetworkingV1alpha1().Ingresses(desired.GetNamespace()).Create(desired.(*netv1alpha1.Ingress))
}

// getIngressForRoute retrieves Ingress for a given route
func (ir *IngressResources) getIngressForRoute(route *v1alpha1.Route) (netv1alpha1.IngressAccessor, error) {
	return ir.ingressLister.Ingresses(route.Namespace).Get(resourcenames.Ingress(route))
}

// updateIngress invokes APIs to updates an Ingress
func (ir *IngressResources) updateIngress(origin netv1alpha1.IngressAccessor) (netv1alpha1.IngressAccessor, error) {
	return ir.servingClientSet.NetworkingV1alpha1().Ingresses(origin.GetNamespace()).Update(origin.(*netv1alpha1.Ingress))
}
