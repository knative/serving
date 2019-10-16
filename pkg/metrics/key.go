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

// Create the tag keys that will be used to add tags to our measurements.
// Tag keys must conform to the restrictions described in
// go.opencensus.io/tag/validate.go. Currently those restrictions are:
// - length between 1 and 255 inclusive
// - characters are printable US-ASCII
var (
	NamespaceTagKey      = tag.MustNewKey(metricskey.LabelNamespaceName)
	ServiceTagKey        = tag.MustNewKey(metricskey.LabelServiceName)
	ConfigTagKey         = tag.MustNewKey(metricskey.LabelConfigurationName)
	RevisionTagKey       = tag.MustNewKey(metricskey.LabelRevisionName)
	PodTagKey            = tag.MustNewKey("pod_name")
	ContainerTagKey      = tag.MustNewKey("container_name")
	ResponseCodeKey      = tag.MustNewKey("response_code")
	ResponseCodeClassKey = tag.MustNewKey("response_code_class")
	NumTriesKey          = tag.MustNewKey("num_tries")

	CommonRevisionKeys = []tag.Key{NamespaceTagKey, ServiceTagKey, ConfigTagKey, RevisionTagKey}
)
