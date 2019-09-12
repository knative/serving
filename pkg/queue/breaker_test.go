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
		"QueueDepth = 0",
		BreakerParams{QueueDepth: 0, MaxConcurrency: 1, InitialCapacity: 1},
	}, {
		"MaxConcurrency negative",
		BreakerParams{QueueDepth: 1, MaxConcurrency: -1, InitialCapacity: 1},
	}, {
		"InitialCapacity negative",
		BreakerParams{QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: -1},
	}, {
		"InitialCapacity out-of-bounds",
		BreakerParams{QueueDepth: 1, MaxConcurrency: 5, InitialCapacity: 6},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("Expected a panic but the code didn't panic.")
				}
			}()

			NewBreaker(test.options)
		})
	}
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

	// The remainer should succeed.
	reqs.processSuccessfully(t)
	reqs.processSuccessfully(t)
}

func TestBreakerQueueing(t *testing.T) {
	params := BreakerParams{QueueDepth: 2, MaxConcurrency: 1, InitialCapacity: 0}
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
	if got, want := b.Capacity(), 1; got != want {
		t.Errorf("Capacity() = %d, want: %d", got, want)
	}

	b.UpdateConcurrency(0)
	if got, want := b.Capacity(), 0; got != want {
		t.Errorf("Capacity() = %d, want: %d", got, want)
	}

	if err := b.UpdateConcurrency(-2); err != ErrUpdateCapacity {
		t.Errorf("UpdateConcurrency = %v, want: %v", err, ErrUpdateCapacity)
	}
}

func TestBreakerUpdateConcurrencyOverlow(t *testing.T) {
	params := BreakerParams{QueueDepth: 1, MaxConcurrency: 1, InitialCapacity: 0}
	b := NewBreaker(params)
	if err := b.UpdateConcurrency(2); err != ErrUpdateCapacity {
		t.Errorf("UpdateConcurrency = %v, want: %v", err, ErrUpdateCapacity)
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

// Test empty semaphore, add capacity, token can be acquired
func TestSemaphoreAcquireHasCapacity(t *testing.T) {
	gotChan := make(chan struct{}, 1)
	want := 1

	sem := newSemaphore(1, 0)
	tryAcquire(sem, gotChan)
	sem.release() // Allows 1 acquire

	for i := 0; i < want; i++ {
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
	if err := sem.release(); err != nil {
		t.Errorf("release = %v; want: %v", err, nil)
	}
	if err := sem.release(); err != ErrRelease {
		t.Errorf("release = %v; want: %v", err, ErrRelease)
	}
}

func TestSemaphoreReleasesSeveralReducers(t *testing.T) {
	const wantAfterFirstrelease = 1
	const wantAfterSecondrelease = 0
	sem := newSemaphore(2, 2)
	sem.acquire(context.Background())
	sem.acquire(context.Background())
	sem.updateCapacity(0)
	sem.release()
	if got := sem.Capacity(); got != wantAfterSecondrelease {
		t.Errorf("Capacity = %d, want: %d", got, wantAfterSecondrelease)
	}
	if sem.reducers != wantAfterFirstrelease {
		t.Errorf("sem.reducers = %d, want: %d", sem.reducers, wantAfterFirstrelease)
	}

	sem.release()
	if got := sem.Capacity(); got != wantAfterSecondrelease {
		t.Errorf("Capacity = %d, want: %d", got, wantAfterSecondrelease)
	}
	if sem.reducers != wantAfterSecondrelease {
		t.Errorf("sem.reducers = %d, want: %d", sem.reducers, wantAfterSecondrelease)
	}
}

func TestSemaphoreUpdateCapacity(t *testing.T) {
	const initialCapacity = 1
	sem := newSemaphore(3, initialCapacity)
	if got, want := sem.Capacity(), 1; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	sem.acquire(context.Background())
	sem.updateCapacity(initialCapacity + 2)
	if got, want := sem.Capacity(), 3; got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
}

// Test the case when we add more capacity then the number of waiting reducers
func TestSemaphoreUpdateCapacityLessThenReducers(t *testing.T) {
	const initialCapacity = 2
	sem := newSemaphore(2, initialCapacity)
	sem.acquire(context.Background())
	sem.acquire(context.Background())
	sem.updateCapacity(initialCapacity - 2)
	if got, want := sem.reducers, 2; got != want {
		t.Errorf("sem.reducers = %d, want: %d", got, want)
	}
	sem.release()
	sem.release()
	sem.release()
	if got, want := sem.reducers, 0; got != want {
		t.Errorf("sem.reducers = %d, want: %d", got, want)
	}
}

func TestSemaphoreUpdateCapacityConsumingReducers(t *testing.T) {
	const initialCapacity = 2
	sem := newSemaphore(2, initialCapacity)
	sem.acquire(context.Background())
	sem.acquire(context.Background())
	sem.updateCapacity(initialCapacity - 2)
	if got, want := sem.reducers, 2; got != want {
		t.Errorf("sem.reducers = %d, want: %d", got, want)
	}

	sem.updateCapacity(initialCapacity)
	if got, want := sem.reducers, 0; got != want {
		t.Errorf("sem.reducers = %d, want: %d", got, want)
	}
}

func TestSemaphoreUpdateCapacityOverflow(t *testing.T) {
	sem := newSemaphore(2, 0)
	if err := sem.updateCapacity(3); err != ErrUpdateCapacity {
		t.Errorf("updateCapacity = %v, want: %v", err, ErrUpdateCapacity)
	}
}

func TestSemaphoreUpdateCapacityOutOfBound(t *testing.T) {
	sem := newSemaphore(1, 1)
	sem.acquire(context.Background())
	if err := sem.updateCapacity(-1); err != ErrUpdateCapacity {
		t.Errorf("updateCapacity = %v, want: %v", err, ErrUpdateCapacity)
	}
}

func TestSemaphoreUpdateCapacityBrokenState(t *testing.T) {
	sem := newSemaphore(1, 0)
	sem.release() // This Release is not paired with an acquire
	if err := sem.updateCapacity(1); err != ErrUpdateCapacity {
		t.Errorf("updateCapacity = %v, want: %v", err, ErrUpdateCapacity)
	}
}

func TestSemaphoreUpdateCapacityDoNothing(t *testing.T) {
	sem := newSemaphore(1, 1)
	if err := sem.updateCapacity(1); err != nil {
		t.Errorf("updateCapacity = %v, want: %v", err, nil)
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
