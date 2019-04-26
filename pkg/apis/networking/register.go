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

package networking

import (
	"time"
)

const (
	// GroupName is the name for the networking API group.
	GroupName = "networking.internal.knative.dev"

	// IngressClassAnnotationKey is the annotation for the
	// explicit class of ClusterIngress that a particular resource has
	// opted into. For example,
	//
	//    networking.knative.dev/ingress.class: some-network-impl
	//
	// This uses a different domain because unlike the resource, it is
	// user-facing.
	//
	// The parent resource may use its own annotations to choose the
	// annotation value for the ClusterIngress it uses.  Based on such
	// value a different reconciliation logic may be used (for examples,
	// Istio-based ClusterIngress will reconcile into a VirtualService).
	IngressClassAnnotationKey = "networking.knative.dev/ingress.class"

	// IngressLabelKey is the label key attached to underlying network programming
	// resources to indicate which ClusterIngress triggered their creation.
	IngressLabelKey = GroupName + "/clusteringress"

	// SKSLabelKey is the label key that SKS Controller attaches to the
	// underlying resources it controls.
	SKSLabelKey = GroupName + "/serverlessservice"

	// ServiceTypeKey is the label key attached to a service specifying the type of service.
	// e.g. Public, Metrics
	ServiceTypeKey = GroupName + "/serviceType"

	// OriginSecretNameLabelKey is the label key attached to the TLS secret to indicate
	// the name of the origin secret that the TLS secret is copied from.
	OriginSecretNameLabelKey = GroupName + "/originSecretName"

	// OriginSecretNamespaceLabelKey is the label key attached to the TLS secret
	// to indicate the namespace of the origin secret that the TLS secret is copied from.
	OriginSecretNamespaceLabelKey = GroupName + "/originSecretNamespace"

	// ServicePortNameHTTP1 is the name of the external port of the service for HTTP/1.1
	ServicePortNameHTTP1 = "http"
	// ServicePortNameH2C is the name of the external port of the service for HTTP/2
	ServicePortNameH2C = "http2"
)

// ServiceType is the enumeration type for the Kubernetes services
// that we have in our system, classified by usage purpose.
type ServiceType string

const (
	// ServiceTypePrivate is the label value for internal only services
	// for user applications.
	ServiceTypePrivate ServiceType = "Private"
	// ServiceTypePublic is the label value for externally reachable
	// services for user applications.
	ServiceTypePublic ServiceType = "Public"
	// ServiceTypeMetrics is the label value for Metrics services. Such services
	// are used for meric scraping.
	ServiceTypeMetrics ServiceType = "Metrics"
)

// Pseudo-constants
var (
	// DefaultTimeout will be set if timeout not specified.
	DefaultTimeout = 10 * time.Minute

	// DefaultRetryCount will be set if Attempts not specified.
	DefaultRetryCount = 3
)

// The ports we setup on our services.
const (
	// ServiceHTTPPort is the port that we setup our Serving and Activator K8s services for
	// HTTP/1 endpoints.
	ServiceHTTPPort = 80
	// ServiceHTTP2Port is the port that we setup our Serving and Activator K8s services for
	// HTTP/2 endpoints.
	ServiceHTTP2Port = 81

	// BackendHTTPPort is the backend, i.e. `targetPort` that we setup for HTTP services.
	BackendHTTPPort = 8012

	// BackendHTTP2Port is the backend, i.e. `targetPort` that we setup for HTTP services.
	BackendHTTP2Port = 8013

	// RequestQueueAdminPort specifies the port number for
	// health check and lifecyle hooks for queue-proxy.
	RequestQueueAdminPort = 8022

	// RequestQueueMetricsPort specifies the port number for metrics emitted
	// by queue-proxy.
	RequestQueueMetricsPort = 9090
)

// ServicePortName returns the port for the app level protocol.
func ServicePortName(proto ProtocolType) string {
	if proto == ProtocolH2C {
		return ServicePortNameH2C
	}
	return ServicePortNameHTTP1
}
