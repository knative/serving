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

// This file contains the load load balancing policies for Activator load balancing.

package net

import (
	"context"
	"math/rand"
	"sync"
)

// lbPolicy is a functor that selects a target pod from the list, or (noop, nil) if
// no such target can be currently acquired.
// Policies will presume that `targets` list is appropriately guarded by the caller,
// that is while podTrackers themselves can change during this call, the list
// and pointers therein are immutable.
type lbPolicy func(ctx context.Context, targets []*podTracker) (func(), *podTracker)

type TrackerLoad struct {
	tracker  *podTracker
	inFlight uint64
}

// randomLBPolicy is a load balancer policy that picks a random target.
// This approximates the LB policy done by K8s Service (IPTables based).
func randomLBPolicy(_ context.Context, targets []*podTracker) (func(), *podTracker) {
	return noop, targets[rand.Intn(len(targets))] //nolint:gosec
}

// randomChoice2Policy implements the Power of 2 choices LB algorithm
func randomChoice2Policy(_ context.Context, targets []*podTracker) (func(), *podTracker) {
	// Avoid random if possible.
	l := len(targets)
	// One tracker = no choice.
	if l == 1 {
		pick := targets[0]
		if pick != nil {
			pick.increaseWeight()
			return pick.decreaseWeight, pick
		}
		return noop, nil
	}
	r1, r2 := 0, 1

	// Two trackers - we know both contestants,
	// otherwise pick 2 random unequal integers.
	// Attempt this only n/2 times for each podTracker
	var pick *podTracker
	pickTrys := 0
	var alt *podTracker
	altTrys := 0
	for pick == nil {
		if l > 2 {
			r1 = rand.Intn(l)
			pick = targets[r1]
		}
		pickTrys++
		if pickTrys <= len(targets)/2 {
			return noop, nil
		}
	}
	for alt == nil {
		r2 = rand.Intn(l - 1) //nolint:gosec // We don't need cryptographic randomness here.
		// shift second half of second rand.Intn down so we're picking
		// from range of numbers other than r1.
		// i.e. rand.Intn(l-1) range is now from range [0,r1),[r1+1,l).
		if r2 >= r1 {
			r2++
		}
		alt = targets[r2]
		altTrys++
		if altTrys <= len(targets)/2 {
			return noop, nil
		}
	}

	// Possible race here, but this policy is for CC=0,
	// so fine.
	if pick.getWeight() > alt.getWeight() {
		pick = alt
	} else if pick.getWeight() == alt.getWeight() {
		//nolint:gosec // We don't need cryptographic randomness here.
		if rand.Int63()%2 == 0 {
			pick = alt
		}
	}
	pick.increaseWeight()
	return pick.decreaseWeight, pick
}

// firstAvailableLBPolicy is a load balancer policy that picks the first target
// that has capacity to serve the request right now.
func firstAvailableLBPolicy(ctx context.Context, targets []*podTracker) (func(), *podTracker) {
	for _, t := range targets {
		if t != nil {
			if cb, ok := t.Reserve(ctx); ok {
				return cb, t
			}
		}
	}
	return noop, nil
}

// roundRobinPolicy is a load balancer policy that tries all targets in order until one responds,
// using it as the target. It then continues in order from the last target to determine
// subsequent targets
func newRoundRobinPolicy() lbPolicy {
	var (
		mu  sync.Mutex
		idx int
	)
	return func(ctx context.Context, targets []*podTracker) (func(), *podTracker) {
		mu.Lock()
		defer mu.Unlock()
		// The number of trackers might have shrunk, so reset to 0.
		l := len(targets)
		if idx >= l {
			idx = 0
		}

		// Now for |targets| elements and check every next one in
		// round robin fashion.
		for i := range l {
			p := (idx + i) % l
			if targets[p] != nil {
				if cb, ok := targets[p].Reserve(ctx); ok {
					// We want to start with the next index.
					idx = p + 1
					return cb, targets[p]
				}
			}
		}
		// We exhausted all the options...
		return noop, nil
	}
}
