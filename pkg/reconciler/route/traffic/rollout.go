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

import (
	"knative.dev/networking/pkg/apis/networking"
)

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

// Roll processes previous Rollout object and compares to the current
// rollout state. If there is different, Merge will start or stop the rollout
// and update `RevisionRollout` objects accoringly.
// Roll returns true if any changes have been made.
func (cur *Rollout) Roll(prev *Rollout) bool {
	if prev == nil {
		return false
	}

	// The algorithm below is simplest, but probably not the most performant.
	// Optimize in the later passes.
	// Map by tag.
	ccfg, pcfg := map[string][]*ConfigurationRollout{}, map[string][]*ConfigurationRollout{}
	for i := range cur.Configurations {
		cfg := &cur.Configurations[i]
		ccfg[cfg.Tag] = append(ccfg[cfg.Tag], cfg)
	}
	for i := range prev.Configurations {
		cfg := &prev.Configurations[i]
		pcfg[cfg.Tag] = append(pcfg[cfg.Tag], cfg)
	}
	ret := false
	for t, cfgs := range ccfg {
		pcfgs, ok := pcfg[t]
		// A new tag was added, so we have no previous state to roll from,
		// thus just move over, we'll rollout to 100% from the get go (and it is
		// always 100%, since default tag is _always_ there).
		if !ok {
			continue
		}
		for i, j := 0, 0; i < len(cfgs) && j < len(pcfgs); {
			switch {
			case cfgs[i].ConfigurationName == pcfgs[j].ConfigurationName:
				// Config might have 0 traffic assigned, if it is a tagged route.
				if cfgs[i].Percent != 0 {
					ret = ret || rollConfig(cfgs[i], pcfgs[j])
				}
				i++
				j++
			case cfgs[i].ConfigurationName < pcfgs[j].ConfigurationName:
				// A new config, has been added. No action for rollout though.
				i++
			default: // cur > prev.
				// A config has been removed. Again, no action for rollout, since
				// this will no longer be rolling out even it were.
				j++
			}
		}
		// Non overlapping configs don't matter since we either roll 100% or remove them.
	}
	return ret
}

func rollConfig(cur, prev *ConfigurationRollout) bool {
	pc := len(prev.Revisions)
	// curr will always have just 1 element – the current desired revision.
	if cur.Revisions[0].RevisionName == prev.Revisions[pc-1].RevisionName {
		// TODO(vagababov): here would go the logic to compute new percentages for the rollout,
		// i.e step function, so return value will change, depending on that.
		// TODO(vagababov): percentage might change, so this should trigger recompute of existing
		// revision rollouts.
		return false
	}

	// Append the new revision, to the list of previous ones, this should start the
	// rollout.
	rev := cur.Revisions[0]
	rev.Percent = 1
	out := make([]RevisionRollout, 0, len(prev.Revisions)+1)
	// Go backwards and find first revision with traffic assignment > 0.
	// Reduce it by one, so we can give that 1% to the new revision.
	for i := len(prev.Revisions) - 1; i >= 0; i-- {
		if pp := prev.Revisions[i].Percent; pp > 0 {
			prev.Revisions[i].Percent--
			break
		}
	}

	// Copy the non 0% objects over.
	for _, r := range prev.Revisions {
		if r.Percent == 0 {
			// Skip the zeroed out items. This can be the 1%->0% from above, but
			// generally speaking should not happen otherwise, aside from
			// users modifying the annotation manually.
			continue
		}
		out = append(out, r)
	}
	// And replace current with the modified previous version.
	cur.Revisions = append(out, rev)
	return true
}
