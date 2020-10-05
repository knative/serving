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

import "k8s.io/apimachinery/pkg/runtime/schema"

const (
	// GroupName is the group name for knative labels and annotations
	GroupName = "serving.knative.dev"

	// GroupNamePrefix is the prefix for label key and annotation key
	GroupNamePrefix = GroupName + "/"

	// ConfigurationLabelKey is the label key attached to a Revision indicating by
	// which Configuration it is created.
	ConfigurationLabelKey = GroupName + "/configuration"

	// RevisionLastPinnedAnnotationKey is the annotation key used for determining when a route has
	// pinned a revision
	RevisionLastPinnedAnnotationKey = GroupName + "/lastPinned"

	// RevisionPreservedAnnotationKey is the annotation key used for preventing garbage collector
	// from automatically deleting the revision.
	RevisionPreservedAnnotationKey = GroupName + "/no-gc"

	// RouteLabelKey is the label key attached to a Configuration indicating by
	// which Route it is configured as traffic target.
	// The key is also attached to Revision resources to indicate they are directly
	// referenced by a Route, or are a child of a Configuration which is referenced by a Route.
	// The key can also be attached to Ingress resources to indicate
	// which Route triggered their creation.
	// The key is also attached to k8s Service resources to indicate which Route
	// triggered their creation.
	RouteLabelKey = GroupName + "/route"

	// RoutesAnnotationKey is an annotation attached to a Revision to indicate that it is
	// referenced by one or many routes. The value is a comma separated list of Route names.
	RoutesAnnotationKey = GroupName + "/routes"

	// RoutingStateLabelKey is the label attached to a Revision indicating
	// its state in relation to serving a Route.
	RoutingStateLabelKey = GroupName + "/routingState"

	// RoutingStateModifiedAnnotationKey indicates the last time the RoutingStateLabel
	// was modified. This is used for ordering when Garbage Collecting old Revisions.
	RoutingStateModifiedAnnotationKey = GroupName + "/routingStateModified"

	// RouteNamespaceLabelKey is the label key attached to a Ingress
	// by a Route to indicate which namespace the Route was created in.
	RouteNamespaceLabelKey = GroupName + "/routeNamespace"

	// RevisionLabelKey is the label key attached to k8s resources to indicate
	// which Revision triggered their creation.
	RevisionLabelKey = GroupName + "/revision"

	// RevisionUID is the label key attached to a revision to indicate
	// its unique identifier
	RevisionUID = GroupName + "/revisionUID"

	// ServiceLabelKey is the label key attached to a Route and Configuration indicating by
	// which Service they are created.
	ServiceLabelKey = GroupName + "/service"

	// ConfigurationGenerationLabelKey is the label key attached to a Revision indicating the
	// metadata generation of the Configuration that created this revision
	ConfigurationGenerationLabelKey = GroupName + "/configurationGeneration"

	// ForceUpgradeAnnotationKey is the annotation which was added to resources
	// upgraded from v1alpha1.
	// This annotation is no longer used since v1alpha1 was removed, but
	// must continue to be allowed since it may be present on existing resources.
	ForceUpgradeAnnotationKey = GroupName + "/forceUpgrade"

	// CreatorAnnotation is the annotation key to describe the user that
	// created the resource.
	CreatorAnnotation = GroupName + "/creator"
	// UpdaterAnnotation is the annotation key to describe the user that
	// last updated the resource.
	UpdaterAnnotation = GroupName + "/lastModifier"

	// QueueSideCarResourcePercentageAnnotation is the percentage of user container resources to be used for queue-proxy
	// It has to be in [0.1,100]
	QueueSideCarResourcePercentageAnnotation = "queue.sidecar." + GroupName + "/resourcePercentage"

	// VisibilityLabelKeyObsolete is the obsolete VisibilityLabelKey.
	// This will move over to VisibilityLabelKey in networking repo..
	VisibilityLabelKeyObsolete = "serving.knative.dev/visibility"

	// VisibilityClusterLocal is the label value for VisibilityLabelKey
	// that will result to the Route/KService getting a cluster local
	// domain suffix.
	VisibilityClusterLocal = "cluster-local"
)

var (
	// ServicesResource represents a Knative Service
	ServicesResource = schema.GroupResource{
		Group:    GroupName,
		Resource: "services",
	}

	// ConfigurationsResource represents a Knative Configuration
	ConfigurationsResource = schema.GroupResource{
		Group:    GroupName,
		Resource: "configurations",
	}

	// RevisionsResource represents a Knative Revision
	RevisionsResource = schema.GroupResource{
		Group:    GroupName,
		Resource: "revisions",
	}

	// RoutesResource represents a Knative Route
	RoutesResource = schema.GroupResource{
		Group:    GroupName,
		Resource: "routes",
	}
)
