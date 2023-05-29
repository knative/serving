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
	for i := 0; i < total; i++ {
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
	}} {
		for _, n := range []int{1, 2, 3, 10, 100} {
			b.Run(fmt.Sprintf("%s-%d-trackers-sequential", test.name, n), func(b *testing.B) {
				targets := makeTrackers(n, 0)
				for i := 0; i < b.N; i++ {
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
