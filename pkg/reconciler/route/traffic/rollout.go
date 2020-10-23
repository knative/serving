/*
Copyright 2020 The Knative Authors

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

// rollout.go contains the types and functions to deal with
// gradual rollout of the new revision for a configuration target.
// The types in this file are expected to be serialized as strings
// and used as annotations for progressive rollout logic.

package traffic

import "knative.dev/networking/pkg/apis/networking"

// RolloutAnnotationKey is the annotation key for storing
// the rollout state in the Annotations of the Kingress or Route.Status.
const RolloutAnnotationKey = networking.GroupName + "/rollout"

// Rollout encapsulates the current rollout state of the system.
// Since the route might reference more than one configuration.
//
// There may be several rollouts going on at the same time for the
// same configuration if there is a tag configured traffic target.
type Rollout struct {
	Configurations []ConfigurationRollout `json:"configurations,omitempty"`
}

// ConfigurationRollout describes the rollout state for a given config+tag pair.
type ConfigurationRollout struct {
	// Name + tag pair uniquely identifies the rollout target.
	// `tag` will be empty, if this is the `DefaultTarget`.
	ConfigurationName string `json:"configurationName"`
	Tag               string `json:"tag,omitempty"`

	// Percent denotes the total percentage for this configuration.
	// The individual percentages of the Revisions below will sum to this
	// number.
	Percent int `json:"percent"`

	// The revisions in the rollout. In steady state this should
	// contain 0 (no revision is ready) or 1 (rollout done).
	// During the actual rollout it will contain N revisions
	// ordered from oldest to the newest.
	// At the end of the rollout the latest (the tail of the list)
	// will receive 100% of the traffic sent to the key.
	// Note: that it is not 100% of the route traffic, in more complex cases.
	Revisions []RevisionRollout `json:"revisions,omitempty"`

	// TODO(vagababov): more rollout fields here, e.g. duration
	// next step time, etc.
}

// RevisionRollout describes the revision in the config rollout.
type RevisionRollout struct {
	// Name of the revision.
	RevisionName string `json:"revisionName"`
	// How much traffic is routed to the revision. This is a share
	// of total Route traffic, not the relative share of configuration
	// target percentage.
	Percent int `json:"percent"`
}
