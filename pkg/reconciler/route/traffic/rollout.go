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
	"sort"

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
	// Configurations are sorted by tag first and within same tag, by configuration name.
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

// Step merges this rollout object with the previous state and
// returns a new Rollout object representing the merged state.
// At the end of the call the returned object will contain the
// desired traffic shape.
// Step will return cur if no previous state was available.
func (cur *Rollout) Step(prev *Rollout) *Rollout {
	if prev == nil || len(prev.Configurations) == 0 {
		return cur
	}

	// The algorithm below is simplest, but probably not the most performant.
	// TODO: optimize in the later passes.

	// Map the configs by tag.
	currConfigs, prevConfigs := map[string][]*ConfigurationRollout{}, map[string][]*ConfigurationRollout{}
	for i, cfg := range cur.Configurations {
		currConfigs[cfg.Tag] = append(currConfigs[cfg.Tag], &cur.Configurations[i])
	}
	for i, cfg := range prev.Configurations {
		prevConfigs[cfg.Tag] = append(prevConfigs[cfg.Tag], &prev.Configurations[i])
	}

	var ret []ConfigurationRollout
	for t, ccfgs := range currConfigs {
		pcfgs, ok := prevConfigs[t]
		// A new tag was added, so we have no previous state to roll from,
		// thus just add it to the return, we'll rollout to 100% from the get go (and it is
		// always 100%, since default tag is _always_ there).
		// So just append to the return list.
		if !ok {
			ret = append(ret, *ccfgs[0])
			continue
		}
		// This is basically an intersect algorithm,
		// It relies on the fact that inputs are sorted.
		i := 0
		for j := 0; i < len(ccfgs) && j < len(pcfgs); {
			switch {
			case ccfgs[i].ConfigurationName == pcfgs[j].ConfigurationName:
				// Config might have 0% traffic assigned, if it is a tag only route (i.e.
				// receives no traffic via default tag).
				if ccfgs[i].Percent != 0 {
					ret = append(ret, *stepConfig(ccfgs[i], pcfgs[j]))
				}
				i++
				j++
			case ccfgs[i].ConfigurationName < pcfgs[j].ConfigurationName:
				// A new config, has been added. No action for rollout though.
				ret = append(ret, *ccfgs[i])
				i++
			default: // cur > prev.
				// A config has been removed during this update.
				// Again, no action for rollout, since this will no longer
				// be rolling it out (or sending traffic to it overall).
				j++
			}
		}
		// Keep the remaining new objects
		for ; i < len(ccfgs); i++ {
			ret = append(ret, *ccfgs[i])
		}
	}
	ro := &Rollout{Configurations: ret}
	// We need to sort the rollout, since we have map iterations in between,
	// which are random.
	sortRollout(ro)
	return ro
}

// stepConfig takes previous and goal configuration shapes and returns a new
// config rollout, after computing the percetage allocations.
func stepConfig(goal, prev *ConfigurationRollout) *ConfigurationRollout {
	pc := len(prev.Revisions)
	ret := *goal
	// goal will always have just one revision in the list – the current desired revision.
	if goal.Revisions[0].RevisionName == prev.Revisions[pc-1].RevisionName {
		// TODO(vagababov): here would go the logic to compute new percentages for the rollout,
		// i.e step function, so return value will change, depending on that.
		// TODO(vagababov): percentage might change, so this should trigger recompute of existing
		// revision rollouts.
		return &ret
	}

	// Append the new revision, to the list of previous ones.
	// This is how we start the rollout.
	rev := goal.Revisions[0]
	rev.Percent = 1
	// Allocate optimistically.
	out := make([]RevisionRollout, 0, len(prev.Revisions)+1)
	// Go backwards and find first revision with traffic assignment > 0.
	// Reduce it by one, so we can give that 1% to the new revision.
	// By design we drain newest revision first.
	for i := len(prev.Revisions) - 1; i >= 0; i-- {
		if prev.Revisions[i].Percent > 0 {
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
	// And replace goal's rollout with the modified previous version.
	ret.Revisions = append(out, rev)
	return &ret
}

// sortRollout sorts the rollout based on tag so it's consistent
// from run to run, since input to the process is map iterator.
func sortRollout(r *Rollout) {
	sort.Slice(r.Configurations, func(i, j int) bool {
		// Sort by tag and within tag sort by config name.
		if r.Configurations[i].Tag == r.Configurations[j].Tag {
			return r.Configurations[i].ConfigurationName < r.Configurations[j].ConfigurationName
		}
		return r.Configurations[i].Tag < r.Configurations[j].Tag
	})
}
