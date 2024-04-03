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

package v1

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	routenames "knative.dev/serving/pkg/reconciler/route/resources/names"
)

// RouteOption enables further configuration of a Route.
type RouteOption func(*v1.Route)

// WithSpecTraffic sets the Route's traffic block to the specified traffic targets.
func WithSpecTraffic(traffic ...v1.TrafficTarget) RouteOption {
	return func(r *v1.Route) {
		r.Spec.Traffic = traffic
	}
}

// WithRouteUID sets the Route's UID
func WithRouteUID(uid types.UID) RouteOption {
	return func(r *v1.Route) {
		r.UID = uid
	}
}

// WithRouteGeneration sets the route's generation
func WithRouteGeneration(generation int64) RouteOption {
	return func(r *v1.Route) {
		r.Generation = generation
	}
}

// WithRouteObservedGeneration sets the route's observed generation to it's generation
func WithRouteObservedGeneration(r *v1.Route) {
	r.Status.ObservedGeneration = r.Generation
}

// WithRouteFinalizer adds the Route finalizer to the Route.
func WithRouteFinalizer(r *v1.Route) {
	r.ObjectMeta.Finalizers = append(r.ObjectMeta.Finalizers, "routes.serving.knative.dev")
}

// WithRouteDeletionTimestamp adds the Route finalizer to the Route.
func WithRouteDeletionTimestamp(t *metav1.Time) RouteOption {
	return func(r *v1.Route) {
		r.ObjectMeta.DeletionTimestamp = t
	}
}

// WithConfigTarget sets the Route's traffic block to point at a particular Configuration.
func WithConfigTarget(config string) RouteOption {
	return WithSpecTraffic(v1.TrafficTarget{
		ConfigurationName: config,
		Percent:           ptr.Int64(100),
	})
}

// WithRevTarget sets the Route's traffic block to point at a particular Revision.
func WithRevTarget(revision string) RouteOption {
	return WithSpecTraffic(v1.TrafficTarget{
		RevisionName: revision,
		Percent:      ptr.Int64(100),
	})
}

// WithStatusTraffic sets the Route's status traffic block to the specified traffic targets.
func WithStatusTraffic(traffic ...v1.TrafficTarget) RouteOption {
	ctx := apis.WithinStatus(context.Background())

	for _, t := range traffic {
		if err := t.Validate(ctx); err != nil {
			panic(err)
		}
	}

	return func(r *v1.Route) {
		r.Status.Traffic = traffic
	}
}

// WithRouteOwnersRemoved clears the owner references of this Route.
func WithRouteOwnersRemoved(r *v1.Route) {
	r.OwnerReferences = nil
}

// MarkServiceNotOwned calls the function of the same name on the Service's status.
func MarkServiceNotOwned(r *v1.Route) {
	r.Status.MarkServiceNotOwned(routenames.K8sService(r))
}

// WithURL sets the .Status.Domain field to the prototypical domain.
func WithURL(r *v1.Route) {
	r.Status.URL = &apis.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s.%s.example.com", r.Name, r.Namespace),
	}
}

// WithHost sets the .Status.Domain field with domain from arg.
func WithHost(host string) RouteOption {
	return func(r *v1.Route) {
		r.Status.Address = &duckv1.Addressable{
			URL: &apis.URL{
				Scheme: "http",
				Host:   host,
			},
		}
	}
}

// WithHTTPSDomain sets the .Status.URL field to a https-domain based on the name and namespace.
func WithHTTPSDomain(r *v1.Route) {
	r.Status.URL = &apis.URL{
		Scheme: "https",
		Host:   fmt.Sprintf("%s.%s.example.com", r.Name, r.Namespace),
	}
}

// WithAddress sets the .Status.Address field to the prototypical internal hostname.
func WithAddress(r *v1.Route) {
	r.Status.Address = &duckv1.Addressable{
		URL: &apis.URL{
			Scheme: "http",
			Host:   network.GetServiceHostname(r.Name, r.Namespace),
		},
	}
}

// WithAnotherDomain sets the .Status.Domain field to an atypical domain.
func WithAnotherDomain(r *v1.Route) {
	r.Status.URL = &apis.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s.%s.another-example.com", r.Name, r.Namespace),
	}
}

// WithLocalDomain sets the .Status.Domain field to use ClusterDomain suffix.
func WithLocalDomain(r *v1.Route) {
	r.Status.URL = &apis.URL{
		Scheme: "http",
		Host:   network.GetServiceHostname(r.Name, r.Namespace),
	}
}

// WithInitRouteConditions initializes the Service's conditions.
func WithInitRouteConditions(rt *v1.Route) {
	rt.Status.InitializeConditions()
}

// WithRouteConditionsExternalDomainTLSDisabled calls MarkTLSNotEnabled with ExternalDomainTLSNotEnabledMessage
// after initialized the Service's conditions.
func WithRouteConditionsExternalDomainTLSDisabled(rt *v1.Route) {
	rt.Status.InitializeConditions()
	rt.Status.MarkTLSNotEnabled(v1.ExternalDomainTLSNotEnabledMessage)
}

// WithRouteConditionsHTTPDowngrade calls MarkHTTPDowngrade after initialized the Service's conditions.
func WithRouteConditionsHTTPDowngrade(rt *v1.Route) {
	rt.Status.InitializeConditions()
	rt.Status.MarkHTTPDowngrade(routenames.Certificate(rt))
}

// MarkTrafficAssigned calls the method of the same name on .Status
func MarkTrafficAssigned(r *v1.Route) {
	r.Status.MarkTrafficAssigned()
}

// MarkRevisionTargetTrafficError calls the method of the same name on .Status
func MarkRevisionTargetTrafficError(reason, msg string) RouteOption {
	return func(r *v1.Route) {
		r.Status.MarkRevisionTargetTrafficError(reason, msg)
	}
}

// MarkCertificateNotReady calls the method of the same name on .Status
func MarkCertificateNotReady(r *v1.Route) {
	r.Status.MarkCertificateNotReady(&netv1alpha1.Certificate{})
}

// MarkCertificateNotOwned calls the method of the same name on .Status
func MarkCertificateNotOwned(r *v1.Route) {
	r.Status.MarkCertificateNotOwned(routenames.Certificate(r))
}

// MarkCertificateReady calls the method of the same name on .Status
func MarkCertificateReady(r *v1.Route) {
	r.Status.MarkCertificateReady(routenames.Certificate(r))
}

// WithReadyCertificateName marks the certificate specified by name as ready.
func WithReadyCertificateName(name string) func(*v1.Route) {
	return func(r *v1.Route) {
		r.Status.MarkCertificateReady(name)
	}
}

// MarkIngressReady propagates a Ready=True Ingress status to the Route.
func MarkIngressReady(r *v1.Route) {
	r.Status.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   "Ready",
				Status: "True",
			}},
		},
	})
}

// MarkInRollout marks the route to be in process of rolling out.
func MarkInRollout(r *v1.Route) {
	r.Status.MarkIngressRolloutInProgress()
}

// MarkIngressNotConfigured calls the method of the same name on .Status
func MarkIngressNotConfigured(r *v1.Route) {
	r.Status.MarkIngressNotConfigured()
}

// WithPropagatedStatus propagates the given IngressStatus into the routes status.
func WithPropagatedStatus(status netv1alpha1.IngressStatus) RouteOption {
	return func(r *v1.Route) {
		r.Status.PropagateIngressStatus(status)
	}
}

// MarkMissingTrafficTarget calls the method of the same name on .Status
func MarkMissingTrafficTarget(kind, revision string) RouteOption {
	return func(r *v1.Route) {
		r.Status.MarkMissingTrafficTarget(kind, revision)
	}
}

// MarkConfigurationNotReady calls the method of the same name on .Status
func MarkConfigurationNotReady(name string) RouteOption {
	return func(r *v1.Route) {
		r.Status.MarkConfigurationNotReady(name)
	}
}

// MarkConfigurationFailed calls the method of the same name on .Status
func MarkConfigurationFailed(name string) RouteOption {
	return func(r *v1.Route) {
		r.Status.MarkConfigurationFailed(name)
	}
}

// WithRouteLabel sets the specified label on the Route.
func WithRouteLabel(labels map[string]string) RouteOption {
	return func(r *v1.Route) {
		r.Labels = labels
	}
}

// WithIngressClass sets the ingress class annotation on the Route.
func WithIngressClass(ingressClass string) RouteOption {
	return func(r *v1.Route) {
		if r.Annotations == nil {
			r.Annotations = make(map[string]string, 1)
		}
		r.Annotations[networking.IngressClassAnnotationKey] = ingressClass
	}
}

// WithRouteAnnotation sets the specified annotation on the Route.
func WithRouteAnnotation(annotations map[string]string) RouteOption {
	return func(r *v1.Route) {
		r.Annotations = annotations
	}
}

// Route creates a route with RouteOptions
func Route(namespace, name string, ro ...RouteOption) *v1.Route {
	r := &v1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	for _, opt := range ro {
		opt(r)
	}
	r.SetDefaults(context.Background())
	return r
}
