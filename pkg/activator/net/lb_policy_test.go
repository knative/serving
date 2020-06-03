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
	"testing"
	"time"

	"knative.dev/serving/pkg/queue"
)

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
