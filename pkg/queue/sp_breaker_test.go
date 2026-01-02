/*
Licensed under the Apache License, Version 2.0
*/

package queue

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/atomic"

	"knative.dev/serving/pkg/queue/spmetricserver"
)

func TestResourceBreakerInvalidConstructor(t *testing.T) {
	tests := []struct {
		name    string
		options ResourceBreakerParams
	}{
		{
			name:    "QueueDepthUnits = 0",
			options: ResourceBreakerParams{QueueDepthUnits: 0, MaxConcurrencyUnits: 1, InitialCapacityUnits: 1},
		},
		{
			name:    "MaxConcurrencyUnits negative",
			options: ResourceBreakerParams{QueueDepthUnits: 1, MaxConcurrencyUnits: -1, InitialCapacityUnits: 1},
		},
		{
			name:    "InitialCapacityUnits negative",
			options: ResourceBreakerParams{QueueDepthUnits: 1, MaxConcurrencyUnits: 1, InitialCapacityUnits: -1},
		},
		{
			name:    "InitialCapacityUnits out-of-bounds",
			options: ResourceBreakerParams{QueueDepthUnits: 1, MaxConcurrencyUnits: 5, InitialCapacityUnits: 6},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("Expected a panic but the code didn't panic.")
				}
			}()

			NewResourceBreaker(test.options)
		})
	}
}

func TestResourceBreakerReserveOverload(t *testing.T) {
	params := ResourceBreakerParams{QueueDepthUnits: 1, MaxConcurrencyUnits: 1, InitialCapacityUnits: 1}
	b := NewResourceBreaker(params) // Breaker capacity = 2 units
	cb1, rr := b.Reserve(context.Background(), 1)
	if !rr {
		t.Fatal("Reserve1 failed")
	}
	_, rr = b.Reserve(context.Background(), 1)
	if rr {
		t.Fatal("Reserve2 was an unexpected success.")
	}
	// Release a slot.
	cb1()
	// And reserve it again.
	cb2, rr := b.Reserve(context.Background(), 1)
	if !rr {
		t.Fatal("Reserve2 failed")
	}
	cb2()
}

func TestResourceBreakerOverloadMixed(t *testing.T) {
	// This tests when reservation and maybe are intermixed.
	params := ResourceBreakerParams{QueueDepthUnits: 2, MaxConcurrencyUnits: 2, InitialCapacityUnits: 2}
	b := NewResourceBreaker(params) // Breaker capacity = 4 units
	reqs := newResourceRequestor(b)

	// Bring breaker to capacity.
	reqs.request(1)
	reqs.request(1)
	// Wait until in-flight units reach 2.
	for b.InFlight() != 2 {
		time.Sleep(time.Millisecond * 2)
	}
	_, rr := b.Reserve(context.Background(), 2)
	if rr {
		t.Fatal("Reserve was an unexpected success.")
	}
	// Open slots.
	reqs.processSuccessfully(t)
	reqs.processSuccessfully(t)
	// Now reservation should work.
	cb, rr := b.Reserve(context.Background(), 2)
	if !rr {
		t.Fatal("Reserve unexpectedly failed")
	}
	// Process the reservation.
	cb()
}

func TestResourceBreakerOverload(t *testing.T) {
	params := ResourceBreakerParams{QueueDepthUnits: 1, MaxConcurrencyUnits: 1, InitialCapacityUnits: 1}
	b := NewResourceBreaker(params) // Breaker capacity = 2 units
	reqs := newResourceRequestor(b)

	// Bring breaker to capacity.
	reqs.request(1)
	reqs.request(1)

	// Overshoot by one unit.
	reqs.request(1)
	reqs.expectFailure(t)

	// The remainder should succeed.
	reqs.processSuccessfully(t)
	reqs.processSuccessfully(t)
}

func TestResourceBreakerQueueing(t *testing.T) {
	params := ResourceBreakerParams{QueueDepthUnits: 2, MaxConcurrencyUnits: 1, InitialCapacityUnits: 0}
	b := NewResourceBreaker(params) // Breaker capacity = 3 units
	reqs := newResourceRequestor(b)

	// Bring breaker to capacity. Doesn't error because queue subsumes these requests.
	reqs.request(1)
	reqs.request(1)

	// Update concurrency to allow the requests to be processed.
	b.UpdateConcurrency(1)

	// They should pass just fine.
	reqs.processSuccessfully(t)
	reqs.processSuccessfully(t)
}

func TestResourceBreakerNoOverload(t *testing.T) {
	params := ResourceBreakerParams{QueueDepthUnits: 1, MaxConcurrencyUnits: 1, InitialCapacityUnits: 1}
	b := NewResourceBreaker(params) // Breaker capacity = 2 units
	reqs := newResourceRequestor(b)

	// Bring request to capacity.
	reqs.request(1)
	reqs.request(1)

	// Process one, send a new one in, at capacity again.
	reqs.processSuccessfully(t)
	reqs.request(1)

	// Process one, send a new one in, at capacity again.
	reqs.processSuccessfully(t)
	reqs.request(1)

	// Process the remainder successfully.
	reqs.processSuccessfully(t)
	reqs.processSuccessfully(t)
}

func TestResourceBreakerCancel(t *testing.T) {
	params := ResourceBreakerParams{QueueDepthUnits: 1, MaxConcurrencyUnits: 1, InitialCapacityUnits: 0}
	b := NewResourceBreaker(params)
	reqs := newResourceRequestor(b)

	// Cancel a request which cannot get capacity.
	ctx1, cancel1 := context.WithCancel(context.Background())
	reqs.requestWithContext(ctx1, 1)
	cancel1()
	reqs.expectFailure(t)

	// This request cannot get capacity either.
	ctx2, cancel2 := context.WithCancel(context.Background())
	reqs.requestWithContext(ctx2, 1)
	cancel2()
	reqs.expectFailure(t)

	// Let through a request with capacity then timeout following request
	b.UpdateConcurrency(1)
	reqs.request(1)

	// Exceed capacity and assert one failure. This makes sure the Breaker is consistently
	// at capacity.
	reqs.request(1)
	reqs.request(1)
	reqs.expectFailure(t)

	// This request cannot get capacity.
	ctx3, cancel3 := context.WithCancel(context.Background())
	reqs.requestWithContext(ctx3, 1)
	cancel3()
	reqs.expectFailure(t)

	// The requests that were put in earlier should succeed.
	reqs.processSuccessfully(t)
	reqs.processSuccessfully(t)
}

func TestResourceBreakerUpdateConcurrency(t *testing.T) {
	params := ResourceBreakerParams{QueueDepthUnits: 1, MaxConcurrencyUnits: 1, InitialCapacityUnits: 0}
	b := NewResourceBreaker(params)
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
func TestResourceSemaphoreAcquireHasNoCapacity(t *testing.T) {
	gotChan := make(chan struct{}, 1)
	spMetricsVal := atomic.Value{}
	spMetricsVal.Store(spmetricserver.SuperPodMetrics{CpuUtilization: 0.0})
	sem := newResourceSemaphore(0, &spMetricsVal, 1)
	tryAcquireResourceSemaphore(sem, gotChan, 1)

	select {
	case <-gotChan:
		t.Error("Token was acquired but shouldn't have been")
	case <-time.After(semNoChangeTimeout):
		// Test succeeds, semaphore didn't change in configured time.
	}
}

func TestResourceSemaphoreAcquireNonBlockingHasNoCapacity(t *testing.T) {
	spMetricsVal := atomic.Value{}
	spMetricsVal.Store(spmetricserver.SuperPodMetrics{CpuUtilization: 0.0})
	sem := newResourceSemaphore(0, &spMetricsVal, 1)
	if sem.tryAcquire(1) {
		t.Error("Should have failed immediately")
	}
}

// Test empty semaphore, add capacity, token can be acquired
func TestResourceSemaphoreAcquireHasCapacity(t *testing.T) {
	gotChan := make(chan struct{}, 1)
	want := 1
	spMetricsVal := atomic.Value{}
	spMetricsVal.Store(spmetricserver.SuperPodMetrics{CpuUtilization: 0.0})
	sem := newResourceSemaphore(0, &spMetricsVal, 1)
	tryAcquireResourceSemaphore(sem, gotChan, 1)
	sem.updateCapacity(1) // Allows 1 unit to be acquired

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

func TestResourceSemaphoreRelease(t *testing.T) {
	spMetricsVal := atomic.Value{}
	spMetricsVal.Store(spmetricserver.SuperPodMetrics{CpuUtilization: 0.0})
	sem := newResourceSemaphore(1, &spMetricsVal, 1)
	sem.acquire(context.Background(), 1)
	func() {
		defer func() {
			if e := recover(); e != nil {
				t.Error("Expected no panic, got message:", e)
			}
			sem.release(1)
		}()
	}()
	func() {
		defer func() {
			if e := recover(); e == nil {
				t.Error("Expected panic, but got none")
			}
		}()
		sem.release(1)
	}()
}

func TestResourceSemaphoreUpdateCapacity(t *testing.T) {
	const initialCapacity = 1
	spMetricsVal := atomic.Value{}
	spMetricsVal.Store(spmetricserver.SuperPodMetrics{CpuUtilization: 0.0})
	sem := newResourceSemaphore(initialCapacity, &spMetricsVal, 1)
	if got, want := sem.Capacity(), uint64(1); got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
	sem.acquire(context.Background(), 1)
	sem.updateCapacity(initialCapacity + 2)
	if got, want := sem.Capacity(), uint64(3); got != want {
		t.Errorf("Capacity = %d, want: %d", got, want)
	}
}

func tryAcquireResourceSemaphore(sem *resourceSemaphore, gotChan chan struct{}, resourceUnits uint64) {
	go func() {
		// blocking until someone puts the token into the semaphore
		err := sem.acquire(context.Background(), resourceUnits)
		if err == nil {
			gotChan <- struct{}{}
		}
	}()
}

// resourceRequestor is a set of test helpers around ResourceBreaker testing.
type resourceRequestor struct {
	breaker    *ResourceBreaker
	acceptedCh chan bool
	barrierCh  chan struct{}
}

func newResourceRequestor(breaker *ResourceBreaker) *resourceRequestor {
	return &resourceRequestor{
		breaker:    breaker,
		acceptedCh: make(chan bool),
		barrierCh:  make(chan struct{}),
	}
}

// request is the same as requestWithContext but with a default context.
func (r *resourceRequestor) request(resourceUnits uint64) {
	r.requestWithContext(context.Background(), resourceUnits)
}

// requestWithContext simulates a request in a separate goroutine.
// The request will either fail immediately (as observable via expectFailure)
// or block until processSuccessfully is called.
func (r *resourceRequestor) requestWithContext(ctx context.Context, resourceUnits uint64) {
	go func() {
		err := r.breaker.Maybe(ctx, resourceUnits, func() {
			<-r.barrierCh
		})
		r.acceptedCh <- err == nil
	}()
}

// expectFailure waits for a request to finish and asserts it to be failed.
func (r *resourceRequestor) expectFailure(t *testing.T) {
	t.Helper()
	if <-r.acceptedCh {
		t.Error("expected request to fail but it succeeded")
	}
}

// processSuccessfully allows a request to pass the barrier, waits for it to
// be finished and asserts it to succeed.
func (r *resourceRequestor) processSuccessfully(t *testing.T) {
	t.Helper()
	r.barrierCh <- struct{}{}
	if !<-r.acceptedCh {
		t.Error("expected request to succeed but it failed")
	}
}

func BenchmarkResourceBreakerMaybe(b *testing.B) {
	op := func() {}

	for _, c := range []int{1, 10, 100, 1000} {
		breaker := NewResourceBreaker(ResourceBreakerParams{
			QueueDepthUnits:      10000000,
			MaxConcurrencyUnits:  c,
			InitialCapacityUnits: c,
		})

		b.Run(fmt.Sprintf("%d-sequential", c), func(b *testing.B) {
			for j := 0; j < b.N; j++ {
				breaker.Maybe(context.Background(), 1, op)
			}
		})

		b.Run(fmt.Sprintf("%d-parallel", c), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					breaker.Maybe(context.Background(), 1, op)
				}
			})
		})
	}
}

func BenchmarkResourceBreakerReserve(b *testing.B) {
	op := func() {}
	breaker := NewResourceBreaker(ResourceBreakerParams{
		QueueDepthUnits:      1,
		MaxConcurrencyUnits:  10000000,
		InitialCapacityUnits: 10000000,
	})

	b.Run("sequential", func(b *testing.B) {
		for j := 0; j < b.N; j++ {
			free, got := breaker.Reserve(context.Background(), 1)
			op()
			if got {
				free()
			}
		}
	})

	b.Run("parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				free, got := breaker.Reserve(context.Background(), 1)
				op()
				if got {
					free()
				}
			}
		})
	})
}
