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

package serving

const (
	GroupName = "serving.knative.dev"

	// ConfigurationLabelKey is the label key attached to a Revision indicating by
	// which Configuration it is created.
	ConfigurationLabelKey = GroupName + "/configuration"

	// ConfigurationGenerationAnnotationKey is the annotation key attached to a Revision indicating the
	// generation of the Configuration that created this revision
	ConfigurationGenerationAnnotationKey = GroupName + "/configurationGeneration"

	// RevisionLastPinnedAnnotationKey is the annotation key used for determining when a route has
	// pinned a revision
	RevisionLastPinnedAnnotationKey = GroupName + "/lastPinned"

	// RouteLabelKey is the label key attached to a Configuration indicating by
	// which Route it is configured as traffic target.
	// The key can also be attached to ClusterIngress resources to indicate
	// which Route triggered their creation.
	RouteLabelKey = GroupName + "/route"

	// RouteNamespaceLabelKey is the label key attached to a ClusterIngress
	// by a Route to indicate which namespace the Route was created in.
	RouteNamespaceLabelKey = GroupName + "/routeNamespace"

	// RevisionLabelKey is the label key attached to k8s resources to indicate
	// which Revision triggered their creation.
	RevisionLabelKey = GroupName + "/revision"

	// RevisionUID is the label key attached to a revision to indicate
	// its unique identifier
	RevisionUID = GroupName + "/revisionUID"

	// AutoscalerLabelKey is the label key attached to a autoscaler pod indicating by
	// which Autoscaler deployment it is created.
	AutoscalerLabelKey = GroupName + "/autoscaler"

	// ServiceLabelKey is the label key attached to a Route and Configuration indicating by
	// which Service they are created.
	ServiceLabelKey = GroupName + "/service"
)
