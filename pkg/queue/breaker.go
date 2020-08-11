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
	"errors"
	"fmt"
	"math"

	"go.uber.org/atomic"
)

var (
	// ErrUpdateCapacity indicates that the capacity could not be updated as wished.
	ErrUpdateCapacity = errors.New("failed to add all capacity to the breaker")
	// ErrRelease indicates that release was called more often than acquire.
	ErrRelease = errors.New("semaphore release error: returned tokens must be <= acquired tokens")
	// ErrRequestQueueFull indicates the breaker queue depth was exceeded.
	ErrRequestQueueFull = errors.New("pending request queue full")
)

// MaxBreakerCapacity is the largest valid value for the MaxConcurrency value of BreakerParams.
// This is limited by the maximum size of a chan struct{} in the current implementation.
const MaxBreakerCapacity = math.MaxInt32

// BreakerParams defines the parameters of the breaker.
type BreakerParams struct {
	QueueDepth      int
	MaxConcurrency  int
	InitialCapacity int
}

// Breaker is a component that enforces a concurrency limit on the
// execution of a function. It also maintains a queue of function
// executions in excess of the concurrency limit. Function call attempts
// beyond the limit of the queue are failed immediately.
type Breaker struct {
	inFlight   atomic.Int64
	totalSlots int64
	sem        *semaphore

	// release is the callback function returned to callers by Reserve to
	// allow the reservation made by Reserve to be released.
	release func()
}

// NewBreaker creates a Breaker with the desired queue depth,
// concurrency limit and initial capacity.
func NewBreaker(params BreakerParams) *Breaker {
	if params.QueueDepth <= 0 {
		panic(fmt.Sprintf("Queue depth must be greater than 0. Got %v.", params.QueueDepth))
	}
	if params.MaxConcurrency < 0 {
		panic(fmt.Sprintf("Max concurrency must be 0 or greater. Got %v.", params.MaxConcurrency))
	}
	if params.InitialCapacity < 0 || params.InitialCapacity > params.MaxConcurrency {
		panic(fmt.Sprintf("Initial capacity must be between 0 and max concurrency. Got %v.", params.InitialCapacity))
	}

	b := &Breaker{
		totalSlots: int64(params.QueueDepth + params.MaxConcurrency),
		sem:        newSemaphore(params.MaxConcurrency, params.InitialCapacity),
	}

	// Allocating the closure returned by Reserve here avoids an allocation in Reserve.
	b.release = func() {
		b.sem.release()
		b.releasePending()
	}

	return b
}

// tryAcquirePending tries to acquire a slot on the pending "queue".
func (b *Breaker) tryAcquirePending() bool {
	// This is an atomic version of:
	//
	// if inFlight == totalSlots {
	//   return false
	// } else {
	//   inFlight++
	//   return true
	// }
	//
	// We can't just use an atomic increment as we need to check if we're
	// "allowed" to increment first. Since a Load and a CompareAndSwap are
	// not done atomically, we need to retry until the CompareAndSwap succeeds
	// (it fails if we're raced to it) or if we don't fulfill the condition
	// anymore.
	for {
		cur := b.inFlight.Load()
		if cur == b.totalSlots {
			return false
		}
		if b.inFlight.CAS(cur, cur+1) {
			return true
		}
	}
}

// releasePending releases a slot on the pending "queue".
func (b *Breaker) releasePending() {
	b.inFlight.Dec()
}

// Reserve reserves an execution slot in the breaker, to permit
// richer semantics in the caller.
// The caller on success must execute the callback when done with work.
func (b *Breaker) Reserve(ctx context.Context) (func(), bool) {
	if !b.tryAcquirePending() {
		return nil, false
	}

	if !b.sem.tryAcquire() {
		b.releasePending()
		return nil, false
	}

	return b.release, true
}

// Maybe conditionally executes thunk based on the Breaker concurrency
// and queue parameters. If the concurrency limit and queue capacity are
// already consumed, Maybe returns immediately without calling thunk. If
// the thunk was executed, Maybe returns true, else false.
func (b *Breaker) Maybe(ctx context.Context, thunk func()) error {
	if !b.tryAcquirePending() {
		return ErrRequestQueueFull
	}

	defer b.releasePending()

	// Wait for capacity in the active queue.
	if err := b.sem.acquire(ctx); err != nil {
		return err
	}
	// Defer releasing capacity in the active.
	// It's safe to ignore the error returned by release since we
	// make sure the semaphore is only manipulated here and acquire
	// + release calls are equally paired.
	defer b.sem.release()

	// Do the thing.
	thunk()
	// Report success
	return nil
}

// InFlight returns the number of requests currently in flight in this breaker.
func (b *Breaker) InFlight() int {
	return int(b.inFlight.Load())
}

// UpdateConcurrency updates the maximum number of in-flight requests.
func (b *Breaker) UpdateConcurrency(size int) error {
	return b.sem.updateCapacity(size)
}

// Capacity returns the number of allowed in-flight requests on this breaker.
func (b *Breaker) Capacity() int {
	return b.sem.Capacity()
}

// newSemaphore creates a semaphore with the desired initial capacity.
func newSemaphore(maxCapacity, initialCapacity int) *semaphore {
	queue := make(chan struct{}, maxCapacity)
	sem := &semaphore{queue: queue}
	sem.updateCapacity(initialCapacity)
	return sem
}

// semaphore is an implementation of a semaphore based on packed integers and a channel.
// state is an int64 that has two int32s packed into it: capacity and inFlight. The
// former specifies how many request are allowed at any given time into the semaphore
// while the latter refers to the currently in-flight requests.
// Packing them both into one int64 allows us to optimize access semantics using atomic
// operations, which can't be guaranteed on 2 individual values.
// The channel is merely used as a vehicle to be able to "wake up" individual goroutines
// if capacity becomes free. It's not consistently used in accordance to actual capacity
// but is rather a communication vehicle to ensure waiting routines are properly worken
// up.
type semaphore struct {
	state atomic.Int64
	queue chan struct{}
}

// tryAcquire receives the token from the semaphore if there's one otherwise returns false.
func (s *semaphore) tryAcquire() bool {
	for {
		old := s.state.Load()
		capacity, in := unpack(old)
		if in >= capacity {
			return false
		}
		in++
		if s.state.CAS(old, pack(capacity, in)) {
			return true
		}
	}
}

// acquire acquires capacity from the semaphore.
func (s *semaphore) acquire(ctx context.Context) error {
	for {
		old := s.state.Load()
		capacity, in := unpack(old)

		if in >= capacity {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-s.queue:
			}
			// Force reload state.
			continue
		}

		in++
		if s.state.CAS(old, pack(capacity, in)) {
			return nil
		}
	}
}

// release releases capacity in the semaphore.
// If the semaphore capacity was reduced in between and as a result inFlight is greater
// than capacity, we don't wake up goroutines as they'd not get any capacity anyway.
func (s *semaphore) release() error {
	for {
		old := s.state.Load()
		capacity, in := unpack(old)
		in--

		if in < 0 {
			return ErrRelease
		}

		if s.state.CAS(old, pack(capacity, in)) {
			if in <= capacity {
				select {
				case s.queue <- struct{}{}:
				default:
				}
			}
			return nil
		}
	}
}

// updateCapacity updates the capacity of the semaphore to the desired size.
func (s *semaphore) updateCapacity(size int) error {
	if size < 0 || size > cap(s.queue) {
		return ErrUpdateCapacity
	}

	s32 := int32(size)
	for {
		old := s.state.Load()
		capacity, in := unpack(old)

		if capacity == s32 {
			// Nothing to do, exit early.
			return nil
		}

		if s.state.CAS(old, pack(s32, in)) {
			for i := int32(0); i < s32-capacity; i++ {
				select {
				case s.queue <- struct{}{}:
				default:
				}
			}
			return nil
		}
	}
}

// Capacity is the effective capacity after taking reducers into account.
func (s *semaphore) Capacity() int {
	capacity, _ := unpack(s.state.Load())
	return int(capacity)
}

// unpack takes an int64 and returns two int32 comprised of the leftmost and the
// rightmost bits respectively.
func unpack(in int64) (int32, int32) {
	return int32(in >> 32), int32(in & 0xffffffff)
}

// pack takes two int32 and packs them into a single int64 at the leftmost and the
// rightmost bits respectively.
func pack(left, right int32) int64 {
	out := int64(left) << 32
	out += int64(right)
	return out
}
