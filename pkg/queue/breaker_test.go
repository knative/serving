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

package queue

import (
	"context"
	"fmt"
	"testing"
	"time"
)

const (
	// semAcquireTimeout is a timeout for tests that try to acquire
	// a token of a semaphore.
	semAcquireTimeout = 10 * time.Second

	// semNoChangeTimeout is some additional wait time after a number
	// of acquires is reached to assert that no more acquires get through.
	semNoChangeTimeout = 50 * time.Millisecond
)

func TestBreakerInvalidConstructor(t *testing.T) {
	tests := []struct {
		name    string
		options BreakerParams
	}{{
		name:    "QueueDepth = 0",
		options: BreakerParams{QueueDepth: 0, MaxConcurrency: 1, InitialCapacity: 1},
	}, {
		name:    "MaxConcurrency negative",
		options: BreakerParams{QueueDepth: 1, MaxConcurrency: -1, InitialCapacity: 1},
	}, {
		name:    "InitialCapacity negative",
		options: BreakerParams{QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: -1},
	}, {
		name:    "InitialCapacity out-of-bounds",
		options: BreakerParams{QueueDepth: 1, MaxConcurrency: 5, InitialCapacity: 6},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("Expected a panic but the code didn't panic.")
				}
			}()

			NewBreaker(test.options)
		})
	}
}

func TestBreakerReserveOverload(t *testing.T) {
	params := BreakerParams{QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: 1}
	b := NewBreaker(params) // Breaker capacity = 2
	cb1, rr := b.Reserve(context.Background())
	if !rr {
		t.Fatal("Reserve1 failed")
	}
	_, rr = b.Reserve(context.Background())
	if rr {
		t.Fatal("Reserve2 was an unexpected success.")
	}
	// Release a slot.
	cb1()
	// And reserve it again.
	cb2, rr := b.Reserve(context.Background())
	if !rr {
		t.Fatal("Reserve2 failed")
	}
	cb2()
}

func TestBreakerOverloadMixed(t *testing.T) {
	// This tests when reservation and maybe are intermised.
	params := BreakerParams{QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: 1}
	b := NewBreaker(params) // Breaker capacity = 2
	reqs := newRequestor(b)

	// Bring breaker to capacity.
	reqs.request()
	// This happens in go-routine, so spin.
	for _, in := unpack(b.sem.state.Load()); in != 1; _, in = unpack(b.sem.state.Load()) {
		time.Sleep(time.Millisecond * 2)
	}
	_, rr := b.Reserve(context.Background())
	if rr {
		t.Fatal("Reserve was an unexpected success.")
	}
	// Open a slot.
	reqs.processSuccessfully(t)
	// Now reservation should work.
	cb, rr := b.Reserve(context.Background())
	if !rr {
		t.Fatal("Reserve unexpectedly failed")
	}
	// Process the reservation.
	cb()
}

func TestBreakerOverload(t *testing.T) {
	params := BreakerParams{QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: 1}
	b := NewBreaker(params) // Breaker capacity = 2
	reqs := newRequestor(b)

	// Bring breaker to capacity.
	reqs.request()
	reqs.request()

	// Overshoot by one.
	reqs.request()
	reqs.expectFailure(t)

	// The remainder should succeed.
	reqs.processSuccessfully(t)
	reqs.processSuccessfully(t)
}

func TestBreakerQueueing(t *testing.T) {
	params := BreakerParams{QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: 0}
	b := NewBreaker(params) // Breaker capacity = 2
	reqs := newRequestor(b)

	// Bring breaker to capacity. Doesn't error because queue subsumes these requests.
	reqs.request()
	reqs.request()

	// Update concurrency to allow the requests to be processed.
	b.UpdateConcurrency(1)

	// They should pass just fine.
	reqs.processSuccessfully(t)
	reqs.processSuccessfully(t)
}

func TestBreakerNoOverload(t *testing.T) {
	params := BreakerParams{QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: 1}
	b := NewBreaker(params) // Breaker capacity = 2
	reqs := newRequestor(b)

	// Bring request to capacity.
	reqs.request()
	reqs.request()

	// Process one, send a new one in, at capacity again.
	reqs.processSuccessfully(t)
	reqs.request()

	// Process one, send a new one in, at capacity again.
	reqs.processSuccessfully(t)
	reqs.request()

	// Process the remainder successfully.
	reqs.processSuccessfully(t)
	reqs.processSuccessfully(t)
}

func TestBreakerCancel(t *testing.T) {
	params := BreakerParams{QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: 0}
	b := NewBreaker(params)
	reqs := newRequestor(b)

	// Cancel a request which cannot get capacity.
	ctx1, cancel1 := context.WithCancel(context.Background())
	reqs.requestWithContext(ctx1)
	cancel1()
	reqs.expectFailure(t)

	// This request cannot get capacity either. This reproduced a bug we had when
	// freeing slots on the pendingRequests channel.
	ctx2, cancel2 := context.WithCancel(context.Background())
	reqs.requestWithContext(ctx2)
	cancel2()
	reqs.expectFailure(t)

	// Let through a request with capacity then timeout following request
	b.UpdateConcurrency(1)
	reqs.request()

	// Exceed capacity and assert one failure. This makes sure the Breaker is consistently
	// at capacity.
	reqs.request()
	reqs.request()
	reqs.expectFailure(t)

	// This request cannot get capacity.
	ctx3, cancel3 := context.WithCancel(context.Background())
	reqs.requestWithContext(ctx3)
	cancel3()
	reqs.expectFailure(t)

	// The requests that were put in earlier should succeed.
	reqs.processSuccessfully(t)
	reqs.processSuccessfully(t)
}

func TestBreakerUpdateConcurrency(t *testing.T) {
	params := BreakerParams{QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: 0}
	b := NewBreaker(params)
	b.UpdateConcurrency(1)
	if got, want := b.Capacity(), uint64(1); got != want {
		t.Errorf("Capacity() = %d, want: %d", got, want)
	}

	b.UpdateConcurrency(0)
	if got, want := b.Capacity(), uint64(0); got != want {
		t.Errorf("Capacity() = %d, want: %d", got, want)
	}
}

// Test empty semaphore, token cannot be acquired
func TestSemaphoreAcquireHasNoCapacity(t *testing.T) {
	gotChan := make(chan struct{}, 1)

	sem := newSemaphore(1, 0)
	tryAcquire(sem, gotChan)

	select {
	case <-gotChan:
		t.Error("Token was acquired but shouldn't have been")
	case <-time.After(semNoChangeTimeout):
		// Test succeeds, semaphore didn't change in configured time.
	}
}

func TestSemaphoreAcquireNonBlockingHasNoCapacity(t *testing.T) {
	sem := newSemaphore(1, 0)
	if sem.tryAcquire() {
		t.Error("Should have failed immediately")
	}
}

// Test empty semaphore, add capacity, token can be acquired
func TestSemaphoreAcquireHasCapacity(t *testing.T) {
	gotChan := make(chan struct{}, 1)
	want := 1

	sem := newSemaphore(1, 0)
	tryAcquire(sem, gotChan)
	sem.updateCapacity(1) // Allows 1 acquire

	for range want {
		select {
		case <-gotChan:
			// Successfully acquired a token.
		case <-time.After(semAcquireTimeout):
			t.Error("Was not able to acquire token before timeout")
		}
	}

	select {
	case <-gotChan:
		t.Errorf("Got more acquires than wanted, want = %d, got at least %d", want, want+1)
	case <-time.After(semNoChangeTimeout):
		// No change happened, success.
	}
}

func TestSemaphoreRelease(t *testing.T) {
	sem := newSemaphore(1, 1)
	sem.acquire(context.Background())
	func() {
		defer func() {
			if e := recover(); e != nil {
				t.Error("Expected no panic, got message:", e)
			}
			sem.release()
		}()
	}()
	func() {
		defer func() {
			if e := recover(); e == nil {
				t.Error("Expected panic, but got none")
			}
		}()
		sem.release()
	}()
}

func TestSemaphoreUpdateCapacity(t *testing.T) {
	const initialCapacity = 1
	sem := newSemaphore(3, initialCapacity)
	if got, want := sem.Capacity(), uint64(1); got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	sem.acquire(context.Background())
	sem.updateCapacity(initialCapacity + 2)
	if got, want := sem.Capacity(), uint64(3); got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
}

func TestPackUnpack(t *testing.T) {
	wantL := uint64(256)
	wantR := uint64(513)

	gotL, gotR := unpack(pack(wantL, wantR))

	if gotL != wantL || gotR != wantR {
		t.Fatalf("Got %d, %d want %d, %d", gotL, gotR, wantL, wantR)
	}
}

func TestBreakerPending(t *testing.T) {
	b := NewBreaker(BreakerParams{
		QueueDepth:      10,
		MaxConcurrency:  5,
		InitialCapacity: 5,
	})

	// Initially, pending should be 0
	if pending := b.Pending(); pending != 0 {
		t.Errorf("Expected initial pending to be 0, got %d", pending)
	}

	// Reserve a slot
	ctx := context.Background()
	release1, ok := b.Reserve(ctx)
	if !ok {
		t.Fatal("Expected first Reserve to succeed")
	}

	// After one reservation, pending should be 1 (tracks all acquired)
	if pending := b.Pending(); pending != 1 {
		t.Errorf("Expected pending to be 1 after first reservation, got %d", pending)
	}

	// Fill up the breaker to capacity
	releases := []func(){release1}
	for i := 1; i < 5; i++ {
		release, ok := b.Reserve(ctx)
		if !ok {
			t.Fatalf("Expected Reserve %d to succeed", i+1)
		}
		releases = append(releases, release)
	}

	// All 5 slots are taken
	if pending := b.Pending(); pending != 5 {
		t.Errorf("Expected pending to be 5 at capacity, got %d", pending)
	}

	// Now the breaker is at capacity, Reserve will fail because tryAcquire fails
	// But we can still track pending up to totalSlots
	for i := 5; i < 15; i++ {
		release, ok := b.Reserve(ctx)
		if ok {
			// This will be false since we're at capacity
			releases = append(releases, release)
		}
	}

	// Still have 5 pending (no new ones could be added)
	if pending := b.Pending(); pending != 5 {
		t.Errorf("Expected pending to still be 5, got %d", pending)
	}

	// Release all in-flight
	for _, release := range releases {
		release()
	}

	// After releasing all, pending should be back to 0
	if pending := b.Pending(); pending != 0 {
		t.Errorf("Expected pending to be 0 after releasing all, got %d", pending)
	}
}

func TestBreakerInFlight(t *testing.T) {
	b := NewBreaker(BreakerParams{
		QueueDepth:      10,
		MaxConcurrency:  5,
		InitialCapacity: 5,
	})

	// Initially, in-flight should be 0
	if inFlight := b.InFlight(); inFlight != 0 {
		t.Errorf("Expected initial in-flight to be 0, got %d", inFlight)
	}

	// Reserve a slot
	ctx := context.Background()
	release1, ok := b.Reserve(ctx)
	if !ok {
		t.Fatal("Expected first Reserve to succeed")
	}

	// After one reservation, in-flight should be 1
	if inFlight := b.InFlight(); inFlight != 1 {
		t.Errorf("Expected in-flight to be 1 after first reservation, got %d", inFlight)
	}

	// Reserve more slots up to capacity
	releases := []func(){release1}
	for i := 1; i < 5; i++ {
		release, ok := b.Reserve(ctx)
		if !ok {
			t.Fatalf("Expected Reserve %d to succeed", i+1)
		}
		releases = append(releases, release)

		// Check in-flight count
		if inFlight := b.InFlight(); inFlight != uint64(i+1) {
			t.Errorf("Expected in-flight to be %d, got %d", i+1, inFlight)
		}
	}

	// At capacity, in-flight should be 5
	if inFlight := b.InFlight(); inFlight != 5 {
		t.Errorf("Expected in-flight to be 5 at capacity, got %d", inFlight)
	}

	// Release one
	releases[0]()

	// After releasing one, in-flight should be 4
	if inFlight := b.InFlight(); inFlight != 4 {
		t.Errorf("Expected in-flight to be 4 after releasing one, got %d", inFlight)
	}

	// Release all remaining
	for i := 1; i < len(releases); i++ {
		releases[i]()
	}

	// After releasing all, in-flight should be 0
	if inFlight := b.InFlight(); inFlight != 0 {
		t.Errorf("Expected in-flight to be 0 after releasing all, got %d", inFlight)
	}
}

func TestBreakerCapacityAsUint64(t *testing.T) {
	b := NewBreaker(BreakerParams{
		QueueDepth:      10,
		MaxConcurrency:  5,
		InitialCapacity: 5,
	})

	// Check initial capacity
	if capacity := b.Capacity(); capacity != 5 {
		t.Errorf("Expected initial capacity to be 5, got %d", capacity)
	}

	// Update capacity
	b.UpdateConcurrency(10)

	// Check updated capacity
	if capacity := b.Capacity(); capacity != 10 {
		t.Errorf("Expected capacity to be 10 after update, got %d", capacity)
	}

	// Update to a larger value
	b.UpdateConcurrency(100)

	// Check larger capacity
	if capacity := b.Capacity(); capacity != 100 {
		t.Errorf("Expected capacity to be 100 after update, got %d", capacity)
	}
}

func TestSemaphoreInFlight(t *testing.T) {
	sem := newSemaphore(10, 5)

	// Initially, in-flight should be 0
	if inFlight := sem.InFlight(); inFlight != 0 {
		t.Errorf("Expected initial in-flight to be 0, got %d", inFlight)
	}

	// Acquire a slot
	ok := sem.tryAcquire()
	if !ok {
		t.Fatal("Expected first acquire to succeed")
	}

	// After one acquisition, in-flight should be 1
	if inFlight := sem.InFlight(); inFlight != 1 {
		t.Errorf("Expected in-flight to be 1 after first acquisition, got %d", inFlight)
	}

	// Acquire more slots
	for i := 1; i < 5; i++ {
		ok := sem.tryAcquire()
		if !ok {
			t.Fatalf("Expected acquire %d to succeed", i+1)
		}

		// Check in-flight count
		if inFlight := sem.InFlight(); inFlight != uint64(i+1) {
			t.Errorf("Expected in-flight to be %d, got %d", i+1, inFlight)
		}
	}

	// At capacity, in-flight should be 5
	if inFlight := sem.InFlight(); inFlight != 5 {
		t.Errorf("Expected in-flight to be 5 at capacity, got %d", inFlight)
	}

	// Release one
	sem.release()

	// After releasing one, in-flight should be 4
	if inFlight := sem.InFlight(); inFlight != 4 {
		t.Errorf("Expected in-flight to be 4 after releasing one, got %d", inFlight)
	}

	// Release all remaining
	for i := 1; i < 5; i++ {
		sem.release()
	}

	// After releasing all, in-flight should be 0
	if inFlight := sem.InFlight(); inFlight != 0 {
		t.Errorf("Expected in-flight to be 0 after releasing all, got %d", inFlight)
	}
}

func tryAcquire(sem *semaphore, gotChan chan struct{}) {
	go func() {
		// blocking until someone puts the token into the semaphore
		sem.acquire(context.Background())
		gotChan <- struct{}{}
	}()
}

// requestor is a set of test helpers around breaker testing.
type requestor struct {
	breaker    *Breaker
	acceptedCh chan bool
	barrierCh  chan struct{}
}

func newRequestor(breaker *Breaker) *requestor {
	return &requestor{
		breaker:    breaker,
		acceptedCh: make(chan bool),
		barrierCh:  make(chan struct{}),
	}
}

// request is the same as requestWithContext but with a default context.
func (r *requestor) request() {
	r.requestWithContext(context.Background())
}

// requestWithContext simulates a request in a separate goroutine. The
// request will either fail immediately (as observable via expectFailure)
// or block until processSuccessfully is called.
func (r *requestor) requestWithContext(ctx context.Context) {
	go func() {
		err := r.breaker.Maybe(ctx, func() {
			<-r.barrierCh
		})
		r.acceptedCh <- err == nil
	}()
}

// expectFailure waits for a request to finish and asserts it to be failed.
func (r *requestor) expectFailure(t *testing.T) {
	t.Helper()
	if <-r.acceptedCh {
		t.Error("expected request to fail but it succeeded")
	}
}

// processSuccessfully allows a request to pass the barrier, waits for it to
// be finished and asserts it to succeed.
func (r *requestor) processSuccessfully(t *testing.T) {
	t.Helper()
	r.barrierCh <- struct{}{}
	if !<-r.acceptedCh {
		t.Error("expected request to succeed but it failed")
	}
}

func BenchmarkBreakerMaybe(b *testing.B) {
	op := func() {}

	for _, c := range []int{1, 10, 100, 1000} {
		breaker := NewBreaker(BreakerParams{QueueDepth: 10000000, MaxConcurrency: c, InitialCapacity: c})

		b.Run(fmt.Sprintf("%d-sequential", c), func(b *testing.B) {
			for range b.N {
				breaker.Maybe(context.Background(), op)
			}
		})

		b.Run(fmt.Sprintf("%d-parallel", c), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					breaker.Maybe(context.Background(), op)
				}
			})
		})
	}
}

func BenchmarkBreakerReserve(b *testing.B) {
	op := func() {}
	breaker := NewBreaker(BreakerParams{QueueDepth: 1, MaxConcurrency: 10000000, InitialCapacity: 10000000})

	b.Run("sequential", func(b *testing.B) {
		for range b.N {
			free, got := breaker.Reserve(context.Background())
			op()
			if got {
				free()
			}
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				free, got := breaker.Reserve(context.Background())
				op()
				if got {
					free()
				}
			}
		})
	})
}
