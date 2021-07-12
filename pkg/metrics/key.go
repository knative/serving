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

package metrics

import (
	"go.opencensus.io/tag"
	"knative.dev/pkg/metrics/metricskey"
)

const (
	// ResourceTypeKnativeRevision is the resource type for Knative revision
	ResourceTypeKnativeRevision = "knative_revision"

	// LabelServiceName is the label for the deployed service name
	LabelServiceName = "service_name"

	// LabelRouteName is the label for immutable name of the route that receives the request
	LabelRouteName = "route_name"

	// LabelRouteTag is the label for immutable name of the route tag that receives the request
	LabelRouteTag = "route_tag"

	// LabelConfigurationName is the label for the configuration which created the monitored revision
	LabelConfigurationName = "configuration_name"

	// LabelRevisionName is the label for the monitored revision
	LabelRevisionName = "revision_name"

	// LabelNamespaceName is the label for immutable name of the namespace that the service is deployed
	LabelNamespaceName = metricskey.LabelNamespaceName

	// LabelContainerName is the container for which the metric is reported.
	LabelContainerName = metricskey.ContainerName

	// LabelPodName is the name of the pod for which the metric is reported.
	LabelPodName = metricskey.PodName

	// LabelResponseCode is the label for the HTTP response status code.
	LabelResponseCode = metricskey.LabelResponseCode

	// LabelResponseCodeClass is the label for the HTTP response status code class. For example, "2xx", "3xx", etc.
	LabelResponseCodeClass = metricskey.LabelResponseCodeClass

	// LabelResponseError is the label for client error. For HTTP, A non-2xx status code doesn't cause an error.
	LabelResponseError = metricskey.LabelResponseError

	// LabelResponseTimeout is the label timeout.
	LabelResponseTimeout = metricskey.LabelResponseTimeout
)

// Create the tag keys that will be used to add tags to our measurements.
// Tag keys must conform to the restrictions described in
// go.opencensus.io/tag/validate.go. Currently those restrictions are:
// - length between 1 and 255 inclusive
// - characters are printable US-ASCII
var (
	PodKey               = tag.MustNewKey(LabelPodName)
	ContainerKey         = tag.MustNewKey(LabelContainerName)
	ResponseCodeKey      = tag.MustNewKey(LabelResponseCode)
	ResponseCodeClassKey = tag.MustNewKey(LabelResponseCodeClass)
	RouteTagKey          = tag.MustNewKey(LabelRouteTag)
)
