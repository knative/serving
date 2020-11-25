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

package networking

import "knative.dev/networking/pkg/apis/networking"

// The ports we setup on our services.
const (
	// BackendHTTPPort is the backend, i.e. `targetPort` that we setup for HTTP/1 services.
	BackendHTTPPort = 8012

	// BackendHTTP2Port is the backend, i.e. `targetPort` that we setup for HTTP/2 services.
	BackendHTTP2Port = 8013

	// QueueAdminPort specifies the port number for
	// health check and lifecycle hooks for queue-proxy.
	QueueAdminPort = 8022

	// AutoscalingQueueMetricsPort specifies the port number for metrics emitted
	// by queue-proxy for autoscaler.
	AutoscalingQueueMetricsPort = 9090

	// UserQueueMetricsPort specifies the port number for metrics emitted
	// by queue-proxy for end user.
	UserQueueMetricsPort = 9091

	// ActivatorServiceName is the name of the activator Kubernetes service.
	ActivatorServiceName = "activator-service"

	// SKSLabelKey is the label key that SKS Controller attaches to the
	// underlying resources it controls.
	SKSLabelKey = networking.GroupName + "/serverlessservice"

	// ServiceTypeKey is the label key attached to a service specifying the type of service.
	// e.g. Public, Private.
	ServiceTypeKey = networking.GroupName + "/serviceType"
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
)
