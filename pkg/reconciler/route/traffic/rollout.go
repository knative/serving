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
	"context"
	"math"
	"sort"
	"time"

	"go.uber.org/zap"
	"knative.dev/pkg/logging"
)

// Rollout encapsulates the current rollout state of the system.
// Since the route might reference more than one configuration.
//
// There may be several rollouts going on at the same time for the
// same configuration if there is a tag configured traffic target.
type Rollout struct {
	// Configurations are sorted by tag first and within same tag, by configuration name.
	Configurations []*ConfigurationRollout `json:"configurations,omitempty"`
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

	// StepParams describes rollout params for the configuration.
	StepParams RolloutParams `json:"stepParams"`
}

// RolloutParams contains the timing and sizing parameters for the
// ConfigurationRollout.
type RolloutParams struct {
	// StartTime is the Unix timestamp in ns by when (+/- reconcile precision)
	// the Rollout has started.
	// This is required to compute step time and deadline.
	StartTime int64 `json:"starttime,omitempty"`

	// NextStepTime is the Unix timestamp in ns when the next
	// rollout step should performed.
	NextStepTime int64 `json:"nextStepTime,omitempty"`

	// StepDuration is the number of nanoseconds between two successive steps
	// of rollout.
	StepDuration int64 `json:"stepDuration,omitempty"`

	// How much traffic to move in a single step.
	StepSize int `json:"stepSize,omitempty"`
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

// RolloutsByTag returns the ConfigurationRollout(s) for the given tag.
func (cur *Rollout) RolloutsByTag(t string) []*ConfigurationRollout {
	// TODO(vagababov): add an intermediate cache later.
	ret := []*ConfigurationRollout{}
	st := sort.Search(len(cur.Configurations), func(i int) bool {
		return cur.Configurations[i].Tag >= t
	})
	// Now append all configs rollouts with given tag.
	// If tag != "", then there'll be only one such entry.
	for ; st < len(cur.Configurations) && cur.Configurations[st].Tag == t; st++ {
		ret = append(ret, cur.Configurations[st])
	}
	return ret
}

// Done returns true if all the Configuration rollouts in this
// Rollout have completed.
func (cur *Rollout) Done() bool {
	for i := range cur.Configurations {
		if !cur.Configurations[i].done() {
			return false
		}
	}
	return true
}

// done returns true if there is no active rollout going on
// for the configuration.
func (cur *ConfigurationRollout) done() bool {
	// Zero or just one revision.
	return len(cur.Revisions) < 2
}

// Validate validates current rollout for inconsistencies.
// This is expected to be invoked after annotation deserialization.
// If it returns false — the deserialized object should be discarded.
func (cur *Rollout) Validate() bool {
	for _, c := range cur.Configurations {
		// Cannot be over 100% in our system.
		if c.Percent > 100 {
			return false
		}
		// Ensure step size is valid.
		if c.StepParams.StepSize < 0 || c.StepParams.StepSize > c.Percent {
			return false
		}
		// If total % values in the revision do not add up — discard.
		tot := 0
		for _, r := range c.Revisions {
			tot += r.Percent
		}
		if tot != c.Percent {
			return false
		}
	}
	return true
}

// ObserveReady traverses the configs and the ones that are in rollout
// but have not observed step time yet, will have it set, to
// max(1, nowTS-cfg.StartTime).
func (cur *Rollout) ObserveReady(ctx context.Context, nowTS int64, durationSecs float64) {
	logger := logging.FromContext(ctx)
	for i := range cur.Configurations {
		c := cur.Configurations[i]
		if c.StepParams.StepDuration == 0 && c.StepParams.StartTime > 0 {
			// In really ceil(nowTS-params.StartTime) should always give 1s, but
			// given possible time drift, we'll ensure that at least 1s is returned.
			minStepSec := math.Max(1, math.Ceil(time.Duration(nowTS-c.StepParams.StartTime).Seconds()))
			c.computeProperties(float64(nowTS), minStepSec, durationSecs)
			logger.Debugf("Computed rollout properties for %s: %#v", c.ConfigurationName, c.StepParams)
		} else {
			logger.Debugf("Existing rollout properties for %s: %#v", c.ConfigurationName, c.StepParams)
		}
	}
}

// Step merges this rollout object with the previous state and
// returns a new Rollout object representing the merged state.
// At the end of the call the returned object will contain the
// desired traffic shape.
// Step will return cur if no previous state was available.
// Second return value is the Unix timestamp in ns of the closest
// rollout action to take or 0, if no rollout is currently scheduled.
func (cur *Rollout) Step(ctx context.Context, prev *Rollout, nowTS int64) (*Rollout, int64) {
	logger := logging.FromContext(ctx)
	if prev == nil || len(prev.Configurations) == 0 {
		logger.Debug("No previous Rollout to Step")
		return cur, 0
	}

	// The algorithm below is simplest, but probably not the most performant.
	// TODO: optimize in the later passes.

	// Map the configs by tag.
	currConfigs, prevConfigs := map[string][]*ConfigurationRollout{}, map[string][]*ConfigurationRollout{}
	for i, cfg := range cur.Configurations {
		currConfigs[cfg.Tag] = append(currConfigs[cfg.Tag], cur.Configurations[i])
	}
	for i, cfg := range prev.Configurations {
		prevConfigs[cfg.Tag] = append(prevConfigs[cfg.Tag], prev.Configurations[i])
	}

	var ret []*ConfigurationRollout
	returnTS := int64(math.MaxInt64)
	for t, ccfgs := range currConfigs {
		pcfgs, ok := prevConfigs[t]
		// A new tag was added, so we have no previous state to roll from,
		// thus just add it to the return, we'll rollout to 100% from the get go (and it is
		// always 100%, since default tag is _always_ there).
		// So just append to the return list.
		// TODO(vagababov): perhaps just ignore and remove from the return?
		if !ok {
			ret = append(ret, ccfgs[0])
			continue
		}
		// This is basically an intersect algorithm,
		// It relies on the fact that inputs are sorted.
		for i, j := 0, 0; i < len(ccfgs); {
			switch {
			case j >= len(pcfgs):
				// Those are the new configs that were added during this reconciliation.
				// So we just copy them to the result.
				ret = append(ret, ccfgs[i])
				i++
			case ccfgs[i].ConfigurationName == pcfgs[j].ConfigurationName:
				// Config might have >0% traffic assigned, unless it is a tag only route (i.e.
				// receives no traffic via default tag). So just skip it from the rollout
				// altogether.
				switch p := ccfgs[i].Percent; {
				case p > 1:
					sc := stepConfig(ccfgs[i], pcfgs[j], nowTS, logger)
					ret = append(ret, sc)
					// Keep the minimum value if it is not 0.
					if nst := sc.StepParams.NextStepTime; nst > 0 && nst < returnTS {
						returnTS = nst
					}
				case p == 1:
					// Skip all the work if it's a common A/B scenario where the test config
					// receives just 1% of traffic.
					ret = append(ret, ccfgs[i])
					// default p == 0 => just ignore for rollout.
				}
				i++
				j++
			case ccfgs[i].ConfigurationName < pcfgs[j].ConfigurationName:
				// A new config, has been added. No action for rollout though.
				// Keep it for future rollout actions.
				if ccfgs[i].Percent != 0 {
					ret = append(ret, ccfgs[i])
				}
				i++
			default: // cur > prev.
				// A config has been removed during this update.
				// Again, no action for rollout, since this will no longer
				// be rolling it out (or sending traffic to it overall).
				j++
			}
		}
	}
	ro := &Rollout{Configurations: ret}
	// We need to sort the rollout, since we have map iterations in between,
	// which are random.
	sortRollout(ro)
	// If no active rollouts return 0.
	if returnTS == math.MaxInt64 {
		returnTS = 0
	}
	return ro, returnTS
}

// adjustPercentage updates the rollout with the new percentage values.
// If new percentage is larger than the previous, the last revision gets
// the difference, if it is decreasing then we start removing traffic from
// the older revisions.
func adjustPercentage(goal int, cr *ConfigurationRollout, logger *zap.SugaredLogger) {
	// Usually changing traffic target %, especially during rollout is a rare
	// occurrence, so log all 3 meaningful cases.
	switch diff := goal - cr.Percent; {
	case goal == 0:
		logger.Infof("Traffic for config %s got assigned 0%%", cr.ConfigurationName)
		cr.Revisions = nil // No traffic, no rollout.
	case diff > 0:
		logger.Infof("Traffic for config %s increased by %d%%, assigning the difference to the latest revision",
			cr.ConfigurationName)
		cr.Revisions[len(cr.Revisions)-1].Percent += diff
	case diff < 0:
		diff = -diff // To make logic more natural.
		logger.Infof("Traffic for config %s decreased by %d%%, removing the difference from the oldest revision(s)",
			cr.ConfigurationName)
		i := 0
		for diff > 0 && i < len(cr.Revisions) {
			if cr.Revisions[i].Percent > diff {
				cr.Revisions[i].Percent -= diff
				break
			}
			diff -= cr.Revisions[i].Percent
			i++
		}
		// Remove the revisions that got cut to 0%.
		cr.Revisions = cr.Revisions[i:]
	default: // diff = 0
		// noop; no log; this is the normal operation.
	}
}

// stepRevisions performs re-adjustment of percentages on the revisions
// to rollout more traffic to the last one.
func stepRevisions(goal *ConfigurationRollout, nowTS int64) {
	// Not yet ready to adjust the steps or we're done
	// (shouldn't really be here, but better be defensive).
	if nowTS < goal.StepParams.NextStepTime || len(goal.Revisions) < 2 {
		return
	}

	revLen := len(goal.Revisions)
	remaining := goal.StepParams.StepSize
	writePos := revLen - 1
	// readPos is guaranteed to be >= 0, due to the check above.
	readPos := revLen - 2

	// If step > totalPercent then remaining will always be > 0
	// even after readPos == -1.
	// This is the case when config's target is reduced below step size.
	// E.g. was: R1 = 40 R2 = 10 Step = 10 Total=60
	// Now = Total = 15;
	// After adjust percentage: R1 = 5 R2 = 10
	// Then after first iteration R1 = 0, remaining = 5.
	// We'll handle this situation below.
	for remaining > 0 && readPos >= 0 {
		// If this revision's allocation is strictly larger than the goal,
		// just subtract the different and we're done.
		if goal.Revisions[readPos].Percent > remaining {
			goal.Revisions[readPos].Percent -= remaining
			break
		}
		// Otherwise subtract what is possible and update
		// write position since this revision will no longer
		// receive traffic.
		remaining -= goal.Revisions[readPos].Percent
		writePos--
		readPos--
	}
	// Copy the last one to the write pos
	goal.Revisions[writePos] = goal.Revisions[revLen-1]

	goal.Revisions[writePos].Percent += goal.StepParams.StepSize
	// This can happen if step is now larger than total allocation, see the
	// note above.
	// E.g. with example above R2 = 20, and ro we have to cap it at 15.
	if goal.Revisions[writePos].Percent > goal.Percent {
		goal.Revisions[writePos].Percent = goal.Percent
	}
	// And cull the tail portion of it.
	goal.Revisions = goal.Revisions[:writePos+1]
	// Also set the next time.
	if len(goal.Revisions) > 1 {
		goal.StepParams.NextStepTime = nowTS + goal.StepParams.StepDuration
	} else {
		// This is the last step, we're done! Clear the params out.
		goal.StepParams = RolloutParams{}
	}
}

// stepConfig takes previous and goal configuration shapes and returns a new
// config rollout, after computing the percetage allocations.
func stepConfig(goal, prev *ConfigurationRollout, nowTS int64, logger *zap.SugaredLogger) *ConfigurationRollout {
	pc := len(prev.Revisions)
	ret := &ConfigurationRollout{
		ConfigurationName: goal.ConfigurationName,
		Tag:               goal.Tag,
		Percent:           goal.Percent,
		Revisions:         goal.Revisions,

		// If there is a new revision, then timing information should be reset.
		// So leave them empty here and populate below, if necessary.
	}

	if len(prev.Revisions) > 0 {
		adjustPercentage(goal.Percent, prev, logger)
	}
	// goal will always have just one revision in the list – the current desired revision.
	// If it matches the last revision of the previous rollout state (or there were no revisions)
	// then no new rollout has begun for this configuration.
	if len(prev.Revisions) == 0 || goal.Revisions[0].RevisionName == prev.Revisions[pc-1].RevisionName {
		logger.Debug("No new revision to roll out for config: ", goal.ConfigurationName)
		// So if |prev.revisions| == 0 => then there was no rollout —
		// nothing is required to step.
		// If |prev.revisions| == 1 and it matches current revision then
		// we're done.
		// Thus we need to perform work only if there is more than one
		// revision in the previous state.
		if len(prev.Revisions) > 1 {
			ret.Revisions = prev.Revisions

			// Copy various rollout stats from the previous when no new revision
			// has been created.
			ret.StepParams = prev.StepParams
			// We might end up here before `ObserveReady` is called.
			// In that case don't step individual revisions just yet.
			if ret.StepParams.StepSize > 0 {
				// adjustPercentage above would've already accounted if target for the
				// whole Configuration changed up or down. So here we should just redistribute
				// the existing values.
				stepRevisions(ret, nowTS)
			}
		}
		return ret
	}

	// Otherwise we start a rollout, which means we need to stamp the starttime,
	// the rest of the fields will remain unset and `ObserveReady` will
	// compute them when the ingress becomes ready.
	logger.Debugf("Starting a new revision rollout for configuration %s and revision %s at %d",
		goal.ConfigurationName, goal.Revisions[0].RevisionName, nowTS)
	ret.StepParams.StartTime = nowTS

	// Go backwards and find first revision with traffic assignment > 0.
	// Reduce it by one, so we can give that 1% to the new revision.
	// By design we drain newest revision first.
	for i := len(prev.Revisions) - 1; i >= 0; i-- {
		if prev.Revisions[i].Percent > 0 {
			prev.Revisions[i].Percent--
			break
		}
	}

	// Allocate optimistically.
	out := make([]RevisionRollout, 0, len(prev.Revisions)+1)

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

	// Append the new revision, to the list of previous ones.
	// This is how we start the rollout.
	goalRev := goal.Revisions[0]
	goalRev.Percent = 1
	ret.Revisions = append(out, goalRev)
	return ret
}

// computeProperties computes the time between steps, each step size
// and next reconcile time. This is invoked when the rollout just starts.
// nowTS current unix timestamp in ns.
// Pre: minStepSec >= 1, in seconds.
// Pre: durationSecs > 1, in seconds.
func (cur *ConfigurationRollout) computeProperties(nowTS, minStepSec, durationSecs float64) {
	// First compute number of steps. If it takes more time to step 1% than the
	// whole allotted time for the rollout, do it in 1 step.
	// Take into account that we already used minStepSecs to move first
	// 1% so the overall rollout duration is shorter by this amount.
	// If it took longer than duration it might be negative, so cap it
	// at 1s, so we just do 1 step.
	durationSecs = math.Max(1, durationSecs-minStepSec)
	numSteps := math.Max(1, durationSecs/minStepSec)

	// The smallest step is 1%, so if we can fit more steps
	// than we have percents, we'll make pf = c.Percent-1 steps
	// each equal to 1%. -1, since we already moved 1% of the traffic.
	remPercent := float64(cur.Percent - 1)
	if remPercent < numSteps {
		numSteps = remPercent
	}

	// We're moving traffic in equal steps.
	// For bigger jumps this might yield slightly bigger moves
	// but rounding down makes 1.9 => 1, which basically doubles the rollout time.
	// E.g. 100% in 4 steps. 1% -> 26% -> 51% -> 76% -> 100%.
	// In addition, ensure that we don't have step size larger than total
	//  percentage for the configuration.
	stepSize := math.Round(remPercent / numSteps)

	// The time we sleep between the steps.
	stepDuration := durationSecs / numSteps * float64(time.Second)

	cur.StepParams.StepDuration = int64(stepDuration)
	cur.StepParams.StepSize = int(stepSize)
	cur.StepParams.NextStepTime = int64(nowTS + stepDuration)
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
