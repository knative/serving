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

package v1alpha1

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	duckv1alpha1 "knative.dev/pkg/apis/duck/v1alpha1"
	duckv1beta1 "knative.dev/pkg/apis/duck/v1beta1"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/networking"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	routenames "knative.dev/serving/pkg/reconciler/route/resources/names"
)

// RouteOption enables further configuration of a Route.
type RouteOption func(*v1alpha1.Route)

// WithSpecTraffic sets the Route's traffic block to the specified traffic targets.
func WithSpecTraffic(traffic ...v1alpha1.TrafficTarget) RouteOption {
	return func(r *v1alpha1.Route) {
		r.Spec.Traffic = traffic
	}
}

// WithRouteUID sets the Route's UID
func WithRouteUID(uid types.UID) RouteOption {
	return func(r *v1alpha1.Route) {
		r.ObjectMeta.UID = uid
	}
}

// WithRouteDeletionTimestamp will set the DeletionTimestamp on the Route.
func WithRouteDeletionTimestamp(r *v1alpha1.Route) {
	t := metav1.NewTime(time.Unix(1e9, 0))
	r.ObjectMeta.SetDeletionTimestamp(&t)
}

// WithRouteFinalizer adds the Route finalizer to the Route.
func WithRouteFinalizer(r *v1alpha1.Route) {
	r.ObjectMeta.Finalizers = append(r.ObjectMeta.Finalizers, "routes.serving.knative.dev")
}

// WithAnotherRouteFinalizer adds a non-Route finalizer to the Route.
func WithAnotherRouteFinalizer(r *v1alpha1.Route) {
	r.ObjectMeta.Finalizers = append(r.ObjectMeta.Finalizers, "another.serving.knative.dev")
}

// WithConfigTarget sets the Route's traffic block to point at a particular Configuration.
func WithConfigTarget(config string) RouteOption {
	return WithSpecTraffic(v1alpha1.TrafficTarget{
		TrafficTarget: v1.TrafficTarget{
			ConfigurationName: config,
			Percent:           ptr.Int64(100),
		},
	})
}

// WithRevTarget sets the Route's traffic block to point at a particular Revision.
func WithRevTarget(revision string) RouteOption {
	return WithSpecTraffic(v1alpha1.TrafficTarget{
		TrafficTarget: v1.TrafficTarget{
			RevisionName: revision,
			Percent:      ptr.Int64(100),
		},
	})
}

// WithStatusTraffic sets the Route's status traffic block to the specified traffic targets.
func WithStatusTraffic(traffic ...v1alpha1.TrafficTarget) RouteOption {
	return func(r *v1alpha1.Route) {
		r.Status.Traffic = traffic
	}
}

// WithRouteOwnersRemoved clears the owner references of this Route.
func WithRouteOwnersRemoved(r *v1alpha1.Route) {
	r.OwnerReferences = nil
}

// MarkServiceNotOwned calls the function of the same name on the Service's status.
func MarkServiceNotOwned(r *v1alpha1.Route) {
	r.Status.MarkServiceNotOwned(routenames.K8sService(r))
}

// WithURL sets the .Status.Domain field to the prototypical domain.
func WithURL(r *v1alpha1.Route) {
	r.Status.URL = &apis.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s.%s.example.com", r.Name, r.Namespace),
	}
}

func WithHTTPSDomain(r *v1alpha1.Route) {
	r.Status.URL = &apis.URL{
		Scheme: "https",
		Host:   fmt.Sprintf("%s.%s.example.com", r.Name, r.Namespace),
	}
}

// WithAddress sets the .Status.Address field to the prototypical internal hostname.
func WithAddress(r *v1alpha1.Route) {
	r.Status.Address = &duckv1alpha1.Addressable{
		Addressable: duckv1beta1.Addressable{
			URL: &apis.URL{
				Scheme: "http",
				Host:   fmt.Sprintf("%s.%s.svc.cluster.local", r.Name, r.Namespace),
			},
		},
	}
}

// WithAnotherDomain sets the .Status.Domain field to an atypical domain.
func WithAnotherDomain(r *v1alpha1.Route) {
	r.Status.URL = &apis.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s.%s.another-example.com", r.Name, r.Namespace),
	}
}

// WithLocalDomain sets the .Status.Domain field to use `svc.cluster.local` suffix.
func WithLocalDomain(r *v1alpha1.Route) {
	r.Status.URL = &apis.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s.%s.svc.cluster.local", r.Name, r.Namespace),
	}
}

// WithInitRouteConditions initializes the Service's conditions.
func WithInitRouteConditions(rt *v1alpha1.Route) {
	rt.Status.InitializeConditions()
}

// MarkTrafficAssigned calls the method of the same name on .Status
func MarkTrafficAssigned(r *v1alpha1.Route) {
	r.Status.MarkTrafficAssigned()
}

// MarkCertificateNotReady calls the method of the same name on .Status
func MarkCertificateNotReady(r *v1alpha1.Route) {
	r.Status.MarkCertificateNotReady(routenames.Certificate(r))
}

// MarkCertificateNotOwned calls the method of the same name on .Status
func MarkCertificateNotOwned(r *v1alpha1.Route) {
	r.Status.MarkCertificateNotOwned(routenames.Certificate(r))
}

// MarkCertificateReady calls the method of the same name on .Status
func MarkCertificateReady(r *v1alpha1.Route) {
	r.Status.MarkCertificateReady(routenames.Certificate(r))
}

// WithReadyCertificateName marks the certificate specified by name as ready.
func WithReadyCertificateName(name string) func(*v1alpha1.Route) {
	return func(r *v1alpha1.Route) {
		r.Status.MarkCertificateReady(name)
	}
}

// MarkIngressReady propagates a Ready=True ClusterIngress status to the Route.
func MarkIngressReady(r *v1alpha1.Route) {
	r.Status.PropagateIngressStatus(netv1alpha1.IngressStatus{
		Status: duckv1.Status{
			Conditions: duckv1.Conditions{{
				Type:   "Ready",
				Status: "True",
			}},
		},
	})
}

// MarkIngressNotConfigured calls the method of the same name on .Status
func MarkIngressNotConfigured(r *v1alpha1.Route) {
	r.Status.MarkIngressNotConfigured()
}

// MarkMissingTrafficTarget calls the method of the same name on .Status
func MarkMissingTrafficTarget(kind, revision string) RouteOption {
	return func(r *v1alpha1.Route) {
		r.Status.MarkMissingTrafficTarget(kind, revision)
	}
}

// MarkConfigurationNotReady calls the method of the same name on .Status
func MarkConfigurationNotReady(name string) RouteOption {
	return func(r *v1alpha1.Route) {
		r.Status.MarkConfigurationNotReady(name)
	}
}

// MarkConfigurationFailed calls the method of the same name on .Status
func MarkConfigurationFailed(name string) RouteOption {
	return func(r *v1alpha1.Route) {
		r.Status.MarkConfigurationFailed(name)
	}
}

// WithRouteLabel sets the specified label on the Route.
func WithRouteLabel(key, value string) RouteOption {
	return func(r *v1alpha1.Route) {
		if r.Labels == nil {
			r.Labels = make(map[string]string)
		}
		r.Labels[key] = value
	}
}

// WithIngressClass sets the ingress class annotation on the Route.
func WithIngressClass(ingressClass string) RouteOption {
	return func(r *v1alpha1.Route) {
		if r.Annotations == nil {
			r.Annotations = make(map[string]string)
		}
		r.Annotations[networking.IngressClassAnnotationKey] = ingressClass
	}
}
