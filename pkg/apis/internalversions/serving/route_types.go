/*
Copyright 2018 The Knative Authors.

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

package serving

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/apis"
	duckv1alpha1 "knative.dev/pkg/apis/duck/v1alpha1"
	duckv1beta1 "knative.dev/pkg/apis/duck/v1beta1"
	"knative.dev/pkg/kmeta"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Route is responsible for configuring ingress over a collection of Revisions.
// Some of the Revisions a Route distributes traffic over may be specified by
// referencing the Configuration responsible for creating them; in these cases
// the Route is additionally responsible for monitoring the Configuration for
// "latest ready" revision changes, and smoothly rolling out latest revisions.
// See also: https://knative.dev/serving/blob/master/docs/spec/overview.md#route
type Route struct {
	metav1.TypeMeta
	// +optional
	metav1.ObjectMeta

	// Spec holds the desired state of the Route (from the client).
	// +optional
	Spec RouteSpec

	// Status communicates the observed state of the Route (from the controller).
	// +optional
	Status RouteStatus
}

// Verify that Route adheres to the appropriate interfaces.
var (
	// Check that we can create OwnerReferences to a Route.
	_ kmeta.OwnerRefable = (*Route)(nil)
)

// TrafficTarget holds a single entry of the routing table for a Route.
type TrafficTarget struct {
	// Name is optionally used to expose a dedicated hostname for referencing this
	// target exclusively. It has the form: {name}.${route.status.domain}
	// +optional
	DeprecatedName string

	// Tag is optionally used to expose a dedicated url for referencing
	// this target exclusively.
	// +optional
	// TODO(mattmoor): Discuss alternative naming options.
	Tag string

	// RevisionName of a specific revision to which to send this portion of
	// traffic.  This is mutually exclusive with ConfigurationName.
	// +optional
	RevisionName string

	// ConfigurationName of a configuration to whose latest revision we will send
	// this portion of traffic. When the "status.latestReadyRevisionName" of the
	// referenced configuration changes, we will automatically migrate traffic
	// from the prior "latest ready" revision to the new one.  This field is never
	// set in Route's status, only its spec.  This is mutually exclusive with
	// RevisionName.
	// +optional
	ConfigurationName string

	// LatestRevision may be optionally provided to indicate that the latest
	// ready Revision of the Configuration should be used for this traffic
	// target.  When provided LatestRevision must be true if RevisionName is
	// empty; it must be false when RevisionName is non-empty.
	// +optional
	LatestRevision *bool

	// Percent specifies percent of the traffic to this Revision or Configuration.
	// This defaults to zero if unspecified.
	// +optional
	Percent *int64

	// URL displays the URL for accessing named traffic targets. URL is displayed in
	// status, and is disallowed on spec. URL must contain a scheme (e.g. http://) and
	// a hostname, but may not contain anything else (e.g. basic auth, url path, etc.)
	// +optional
	URL *apis.URL
}

// RouteSpec holds the desired state of the Route (from the client).
type RouteSpec struct {
	// DeprecatedGeneration was used prior in Kubernetes versions <1.11
	// when metadata.generation was not being incremented by the api server
	//
	// This property will be dropped in future Knative releases and should
	// not be used - use metadata.generation
	//
	// Tracking issue: https://knative.dev/serving/issues/643
	//
	// +optional
	DeprecatedGeneration int64

	// Traffic specifies how to distribute traffic over a collection of Knative Serving Revisions and Configurations.
	// +optional
	Traffic []TrafficTarget
}

const (
	// RouteConditionReady is set when the service is configured
	// and has available backends ready to receive traffic.
	RouteConditionReady = apis.ConditionReady

	// RouteConditionAllTrafficAssigned is set to False when the
	// service is not configured properly or has no available
	// backends ready to receive traffic.
	RouteConditionAllTrafficAssigned apis.ConditionType = "AllTrafficAssigned"

	// RouteConditionIngressReady is set to False when the
	// ClusterIngress fails to become Ready.
	RouteConditionIngressReady apis.ConditionType = "IngressReady"

	// RouteConditionCertificateProvisioned is set to False when the
	// Knative Certificates fail to be provisioned for the Route.
	RouteConditionCertificateProvisioned apis.ConditionType = "CertificateProvisioned"
)

// RouteStatusFields holds all of the non-duckv1beta1.Status status fields of a Route.
// These are defined outline so that we can also inline them into Service, and more easily
// copy them.
type RouteStatusFields struct {
	// URL holds the url that will distribute traffic over the provided traffic targets.
	// It generally has the form http[s]://{route-name}.{route-namespace}.{cluster-level-suffix}
	// +optional
	URL *apis.URL

	// DeprecatedDomain holds the top-level domain that will distribute traffic over the provided targets.
	// It generally has the form {route-name}.{route-namespace}.{cluster-level-suffix}
	// +optional
	DeprecatedDomain string

	// DeprecatedDomainInternal holds the top-level domain that will distribute traffic over the provided
	// targets from inside the cluster. It generally has the form
	// {route-name}.{route-namespace}.svc.{cluster-domain-name}
	// DEPRECATED: Use Address instead.
	// +optional
	DeprecatedDomainInternal string

	// Address holds the information needed for a Route to be the target of an event.
	// +optional
	Address *duckv1alpha1.Addressable

	// Traffic holds the configured traffic distribution.
	// These entries will always contain RevisionName references.
	// When ConfigurationName appears in the spec, this will hold the
	// LatestReadyRevisionName that we last observed.
	// +optional
	Traffic []TrafficTarget
}

// RouteStatus communicates the observed state of the Route (from the controller).
type RouteStatus struct {
	duckv1beta1.Status

	RouteStatusFields
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RouteList is a list of Route resources
type RouteList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []Route
}
