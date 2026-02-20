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

package net

import (
	"context"
	"fmt"
	"testing"
	"time"

	"knative.dev/serving/pkg/queue"
)

func TestRandomChoice_TwoTrackersDistribution(t *testing.T) {
	podTrackers := makeTrackers(2, 0)
	counts := map[string]int{}

	total := 100
	for range total {
		cb, pt := randomChoice2Policy(context.Background(), podTrackers)
		cb()
		counts[pt.dest]++
	}

	first := counts[podTrackers[0].dest]
	second := counts[podTrackers[1].dest]

	// probability of this occurring is 0.5^100
	if first == 0 {
		t.Error("expected the first tracker to get some requests")
	}
	if second == 0 {
		t.Error("expected the second tracker to get some requests")
	}
	if first+second != total {
		t.Error("expected total requests to equal 100 - was ", first+second)
	}
}

func TestRandomChoice2(t *testing.T) {
	t.Run("1 tracker", func(t *testing.T) {
		podTrackers := makeTrackers(1, 0)
		cb, pt := randomChoice2Policy(context.Background(), podTrackers)
		t.Cleanup(cb)
		if got, want := pt.dest, podTrackers[0].dest; got != want {
			t.Errorf("pt.dest = %s, want: %s", got, want)
		}
		wantW := int32(1) // to avoid casting on every check.
		if got, want := pt.getWeight(), wantW; got != want {
			t.Errorf("pt.weight = %d, want: %d", got, want)
		}
		cb, pt = randomChoice2Policy(context.Background(), podTrackers)
		if got, want := pt.dest, podTrackers[0].dest; got != want {
			t.Errorf("pt.dest = %s, want: %s", got, want)
		}
		if got, want := pt.getWeight(), wantW+1; got != want {
			t.Errorf("pt.weight = %d, want: %d", got, want)
		}
		cb()
		if got, want := pt.getWeight(), wantW; got != want {
			t.Errorf("pt.weight = %d, want: %d", got, want)
		}
	})
	t.Run("2 trackers", func(t *testing.T) {
		podTrackers := makeTrackers(2, 0)
		cb, pt := randomChoice2Policy(context.Background(), podTrackers)
		t.Cleanup(cb)
		wantW := int32(1) // to avoid casting on every check.
		if got, want := pt.getWeight(), wantW; got != want {
			t.Errorf("pt.weight = %d, want: %d", got, want)
		}
		// Must return a different one.
		cb, pt = randomChoice2Policy(context.Background(), podTrackers)
		dest := pt.dest
		if got, want := pt.getWeight(), wantW; got != want {
			t.Errorf("pt.weight = %d, want: %d", got, want)
		}
		cb()
		// Should return the same one.
		_, pt = randomChoice2Policy(context.Background(), podTrackers)
		if got, want := pt.getWeight(), wantW; got != want {
			t.Errorf("pt.weight = %d, want: %d", got, want)
		}
		if got, want := pt.dest, dest; got != want {
			t.Errorf("pt.dest = %s, want: %s", got, want)
		}
	})
	t.Run("3 trackers", func(t *testing.T) {
		podTrackers := makeTrackers(3, 0)
		cb, pt := randomChoice2Policy(context.Background(), podTrackers)
		t.Cleanup(cb)
		wantW := int32(1) // to avoid casting on every check.
		if got, want := pt.getWeight(), wantW; got != want {
			t.Errorf("pt.weight = %d, want: %d", got, want)
		}
		// Must return a different one.
		cb, pt = randomChoice2Policy(context.Background(), podTrackers)
		if got, want := pt.getWeight(), wantW; got != want {
			t.Errorf("pt.weight = %d, want: %d", got, want)
		}
		cb()
		// Should return same or the other unsued one.
		_, pt = randomChoice2Policy(context.Background(), podTrackers)
		if got, want := pt.getWeight(), wantW; got != want {
			t.Errorf("pt.weight = %d, want: %d", got, want)
		}
	})
}

func TestFirstAvailable(t *testing.T) {
	t.Run("1 tracker, 1 slot", func(t *testing.T) {
		podTrackers := []*podTracker{{
			dest: "this-is-nowhere",
			b: queue.NewBreaker(queue.BreakerParams{
				QueueDepth:      1,
				MaxConcurrency:  1,
				InitialCapacity: 1,
			}),
		}}

		ctx := context.Background()
		cb, tracker := firstAvailableLBPolicy(ctx, podTrackers)
		defer cb()
		if tracker == nil {
			t.Fatal("Tracker was nil")
		}

		ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()

		cb, tracker = firstAvailableLBPolicy(ctx, podTrackers)
		defer cb()
		if tracker != nil {
			t.Fatal("Tracker was not nil")
		}
	})
	t.Run("2 trackers, 1 slot", func(t *testing.T) {
		podTrackers := []*podTracker{{
			dest: "down-by-the-river",
			b: queue.NewBreaker(queue.BreakerParams{
				QueueDepth:      1,
				MaxConcurrency:  1,
				InitialCapacity: 1,
			}),
		}, {
			dest: "heart-of-gold",
			b: queue.NewBreaker(queue.BreakerParams{
				QueueDepth:      1,
				MaxConcurrency:  1,
				InitialCapacity: 1,
			}),
		}}

		ctx := context.Background()
		cb, tracker := firstAvailableLBPolicy(ctx, podTrackers)
		defer cb()
		if tracker == nil {
			t.Fatal("Tracker was nil")
		} else if got, want := tracker.dest, podTrackers[0].dest; got != want {
			t.Errorf("Tracker = %s, want: %s", got, want)
		}

		cb, tracker = firstAvailableLBPolicy(ctx, podTrackers)
		defer cb()
		if tracker == nil {
			t.Fatal("Tracker was nil")
		} else if got, want := tracker.dest, podTrackers[1].dest; got != want {
			t.Errorf("Tracker = %s, want: %s", got, want)
		}
	})
}

func TestRoundRobin(t *testing.T) {
	t.Run("with cc=1", func(t *testing.T) {
		rrp := newRoundRobinPolicy()
		podTrackers := makeTrackers(3, 1)
		cb, pt := rrp(context.Background(), podTrackers)
		t.Cleanup(cb)
		if got, want := pt, podTrackers[0]; got != want {
			t.Fatalf("Tracker = %v, want: %v", got, want)
		}
		cb, pt = rrp(context.Background(), podTrackers)
		t.Cleanup(cb)
		if got, want := pt, podTrackers[1]; got != want {
			t.Fatalf("Tracker = %v, want: %v", got, want)
		}
		// This will make it use shorter array, jump over and start from 0.
		// But it's occupied already, so will fail to acquire.
		_, pt = rrp(context.Background(), podTrackers[:1])
		if pt != nil {
			t.Fatal("Wanted nil, got: ", pt)
		}

		cb, pt = rrp(context.Background(), podTrackers)
		if got, want := pt, podTrackers[2]; got != want {
			t.Fatalf("Tracker = %v, want: %v", got, want)
		}
		_, pt = rrp(context.Background(), podTrackers)
		if pt != nil {
			t.Fatal("Wanted nil, got: ", pt)
		}

		// Reset last one.
		cb()
		cb, pt = rrp(context.Background(), podTrackers)
		if got, want := pt, podTrackers[2]; got != want {
			t.Fatalf("Tracker = %v, want: %v", got, want)
		}
		t.Cleanup(cb)
	})
	t.Run("with cc=2", func(t *testing.T) {
		rrp := newRoundRobinPolicy()
		podTrackers := makeTrackers(3, 2)
		cb, pt := rrp(context.Background(), podTrackers)
		t.Cleanup(cb)
		if got, want := pt, podTrackers[0]; got != want {
			t.Fatalf("Tracker = %v, want: %v", got, want)
		}
		cb, pt = rrp(context.Background(), podTrackers)
		t.Cleanup(cb)
		if got, want := pt, podTrackers[1]; got != want {
			t.Fatalf("Tracker = %v, want: %v", got, want)
		}
		// This will make it use shorter array, jump over and start from 0.
		cb, pt = rrp(context.Background(), podTrackers[:1])
		t.Cleanup(cb)
		if got, want := pt, podTrackers[0]; got != want {
			t.Fatalf("Tracker = %v, want: %v", got, want)
		}
		cb, pt = rrp(context.Background(), podTrackers)
		t.Cleanup(cb)
		if got, want := pt, podTrackers[1]; got != want {
			t.Fatalf("Tracker = %v, want: %v", got, want)
		}
		cb, pt = rrp(context.Background(), podTrackers)
		t.Cleanup(cb)
		if got, want := pt, podTrackers[2]; got != want {
			t.Fatalf("Tracker = %v, want: %v", got, want)
		}
		// Now index 0 has already 2 slots occupied, so is 1, so we should get 2 again.
		cb, pt = rrp(context.Background(), podTrackers)
		t.Cleanup(cb)
		if got, want := pt, podTrackers[2]; got != want {
			t.Fatalf("Tracker = %v, want: %v", got, want)
		}
	})
}

func TestLeastConnectionsPolicy(t *testing.T) {
	t.Run("empty trackers", func(t *testing.T) {
		cb, pt := leastConnectionsPolicy(context.Background(), []*podTracker{})
		defer cb()
		if pt != nil {
			t.Fatal("Expected nil tracker for empty input")
		}
	})

	t.Run("single tracker", func(t *testing.T) {
		podTrackers := makeTrackers(1, 1)
		cb, pt := leastConnectionsPolicy(context.Background(), podTrackers)
		defer cb()
		if pt == nil {
			t.Fatal("Expected non-nil tracker")
		}
		if got, want := pt.dest, podTrackers[0].dest; got != want {
			t.Errorf("pt.dest = %s, want: %s", got, want)
		}
	})

	t.Run("multiple trackers with different loads", func(t *testing.T) {
		podTrackers := makeTrackers(3, 2)
		// Simulate different loads
		podTrackers[0].weight.Store(5)
		podTrackers[1].weight.Store(2)
		podTrackers[2].weight.Store(8)

		cb, pt := leastConnectionsPolicy(context.Background(), podTrackers)
		defer cb()
		if pt == nil {
			t.Fatal("Expected non-nil tracker")
		}
		// Should pick the one with lowest weight (index 1)
		if got, want := pt.dest, podTrackers[1].dest; got != want {
			t.Errorf("pt.dest = %s, want: %s (should pick lowest load)", got, want)
		}
	})

	t.Run("nil trackers in list", func(t *testing.T) {
		podTrackers := []*podTracker{
			nil,
			{
				dest: "tracker-1",
				b: queue.NewBreaker(queue.BreakerParams{
					QueueDepth:      1,
					MaxConcurrency:  1,
					InitialCapacity: 1,
				}),
			},
			nil,
		}
		cb, pt := leastConnectionsPolicy(context.Background(), podTrackers)
		defer cb()
		if pt == nil {
			t.Fatal("Expected non-nil tracker")
		}
		if got, want := pt.dest, "tracker-1"; got != want {
			t.Errorf("pt.dest = %s, want: %s", got, want)
		}
	})

	t.Run("all nil trackers", func(t *testing.T) {
		podTrackers := []*podTracker{nil, nil, nil}
		cb, pt := leastConnectionsPolicy(context.Background(), podTrackers)
		defer cb()
		if pt != nil {
			t.Fatal("Expected nil tracker when all trackers are nil")
		}
	})

	t.Run("negative weight handling", func(t *testing.T) {
		podTrackers := makeTrackers(2, 1)
		podTrackers[0].weight.Store(-5)
		podTrackers[1].weight.Store(3)

		cb, pt := leastConnectionsPolicy(context.Background(), podTrackers)
		defer cb()
		if pt == nil {
			t.Fatal("Expected non-nil tracker")
		}
		// Negative weight should be treated as 0, so should pick first tracker
		if got, want := pt.dest, podTrackers[0].dest; got != want {
			t.Errorf("pt.dest = %s, want: %s (negative weight should be treated as 0)", got, want)
		}
	})
}

func TestRandomLBPolicyWithNilTrackers(t *testing.T) {
	t.Run("empty trackers", func(t *testing.T) {
		cb, pt := randomLBPolicy(context.Background(), []*podTracker{})
		defer cb()
		if pt != nil {
			t.Fatal("Expected nil tracker for empty input")
		}
	})

	t.Run("all nil trackers", func(t *testing.T) {
		podTrackers := []*podTracker{nil, nil, nil}
		cb, pt := randomLBPolicy(context.Background(), podTrackers)
		defer cb()
		if pt != nil {
			t.Fatal("Expected nil tracker when all trackers are nil")
		}
	})

	t.Run("mixed nil and valid trackers", func(t *testing.T) {
		podTrackers := makeTrackers(3, 0)
		// Set middle one to nil
		podTrackers[1] = nil

		// Run multiple times to ensure we don't get nil
		for range 10 {
			cb, pt := randomLBPolicy(context.Background(), podTrackers)
			defer cb()
			if pt == nil {
				t.Fatal("Should not return nil when valid trackers exist")
			}
			if pt.dest != podTrackers[0].dest && pt.dest != podTrackers[2].dest {
				t.Fatal("Should return one of the valid trackers")
			}
		}
	})
}

func TestRandomChoice2PolicyWithNilTrackers(t *testing.T) {
	t.Run("single nil tracker", func(t *testing.T) {
		podTrackers := []*podTracker{nil}
		cb, pt := randomChoice2Policy(context.Background(), podTrackers)
		defer cb()
		if pt != nil {
			t.Fatal("Expected nil tracker when single tracker is nil")
		}
	})

	t.Run("all nil trackers", func(t *testing.T) {
		podTrackers := []*podTracker{nil, nil, nil}
		cb, pt := randomChoice2Policy(context.Background(), podTrackers)
		defer cb()
		if pt != nil {
			t.Fatal("Expected nil tracker when all trackers are nil")
		}
	})

	t.Run("mixed nil and valid trackers", func(t *testing.T) {
		podTrackers := makeTrackers(4, 0)
		// Set some to nil
		podTrackers[1] = nil
		podTrackers[3] = nil

		// Run multiple times to check behavior
		foundNonNil := false
		for range 20 {
			cb, pt := randomChoice2Policy(context.Background(), podTrackers)
			defer cb()
			if pt != nil {
				foundNonNil = true
				if pt.dest != podTrackers[0].dest && pt.dest != podTrackers[2].dest {
					t.Fatal("Should return one of the valid trackers")
				}
			}
		}
		if !foundNonNil {
			t.Fatal("Should find at least one non-nil tracker in multiple attempts")
		}
	})

	t.Run("mostly nil trackers", func(t *testing.T) {
		// Create a large array with mostly nils
		podTrackers := make([]*podTracker, 10)
		// Create a proper tracker with initialized fields
		validTracker := &podTracker{
			dest: "valid-tracker",
		}
		// Initialize the weight field properly
		validTracker.weight.Store(0)
		podTrackers[0] = validTracker

		// Run multiple times - should eventually find the valid tracker
		foundValid := false
		for range 100 {
			cb, pt := randomChoice2Policy(context.Background(), podTrackers)
			if cb != nil {
				defer cb()
			}
			if pt != nil && pt.dest == "valid-tracker" {
				foundValid = true
				break
			}
		}
		if !foundValid {
			t.Fatal("Should eventually find the valid tracker")
		}
	})
}

func TestFirstAvailableWithNilTrackers(t *testing.T) {
	t.Run("nil trackers in list", func(t *testing.T) {
		podTrackers := []*podTracker{
			nil,
			{
				dest: "tracker-1",
				b: queue.NewBreaker(queue.BreakerParams{
					QueueDepth:      1,
					MaxConcurrency:  1,
					InitialCapacity: 1,
				}),
			},
			nil,
		}
		cb, pt := firstAvailableLBPolicy(context.Background(), podTrackers)
		defer cb()
		if pt == nil {
			t.Fatal("Expected non-nil tracker")
		}
		if got, want := pt.dest, "tracker-1"; got != want {
			t.Errorf("pt.dest = %s, want: %s", got, want)
		}
	})

	t.Run("all nil trackers", func(t *testing.T) {
		podTrackers := []*podTracker{nil, nil, nil}
		cb, pt := firstAvailableLBPolicy(context.Background(), podTrackers)
		defer cb()
		if pt != nil {
			t.Fatal("Expected nil tracker when all trackers are nil")
		}
	})
}

func TestRoundRobinWithNilTrackers(t *testing.T) {
	t.Run("nil trackers in list", func(t *testing.T) {
		rrp := newRoundRobinPolicy()
		podTrackers := makeTrackers(3, 1)
		// Set middle tracker to nil
		podTrackers[1] = nil

		cb, pt := rrp(context.Background(), podTrackers)
		t.Cleanup(cb)
		if got, want := pt, podTrackers[0]; got != want {
			t.Fatalf("Tracker = %v, want: %v", got, want)
		}

		// Should skip nil tracker and go to next valid one
		cb, pt = rrp(context.Background(), podTrackers)
		t.Cleanup(cb)
		if got, want := pt, podTrackers[2]; got != want {
			t.Fatalf("Tracker = %v, want: %v (should skip nil tracker)", got, want)
		}
	})

	t.Run("all nil trackers", func(t *testing.T) {
		rrp := newRoundRobinPolicy()
		podTrackers := []*podTracker{nil, nil, nil}

		cb, pt := rrp(context.Background(), podTrackers)
		defer cb()
		if pt != nil {
			t.Fatal("Expected nil tracker when all trackers are nil")
		}
	})
}

func BenchmarkPolicy(b *testing.B) {
	for _, test := range []struct {
		name   string
		policy lbPolicy
	}{{
		name:   "random",
		policy: randomLBPolicy,
	}, {
		name:   "random-power-of-2-choice",
		policy: randomChoice2Policy,
	}, {
		name:   "first-available",
		policy: firstAvailableLBPolicy,
	}, {
		name:   "round-robin",
		policy: newRoundRobinPolicy(),
	}, {
		name:   "least-connections",
		policy: leastConnectionsPolicy,
	}} {
		for _, n := range []int{1, 2, 3, 10, 100} {
			b.Run(fmt.Sprintf("%s-%d-trackers-sequential", test.name, n), func(b *testing.B) {
				targets := makeTrackers(n, 0)
				for range b.N {
					cb, _ := test.policy(nil, targets)
					cb()
				}
			})

			b.Run(fmt.Sprintf("%s-%d-trackers-parallel", test.name, n), func(b *testing.B) {
				targets := makeTrackers(n, 0)
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						cb, _ := test.policy(nil, targets)
						cb()
					}
				})
			})
		}
	}
}
