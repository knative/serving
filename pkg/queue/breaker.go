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
	"sync"
	"sync/atomic"
)

var (
	// ErrUpdateCapacity indicates that the capacity could not be updated as wished.
	ErrUpdateCapacity = errors.New("failed to add all capacity to the breaker")
	// ErrRelease indicates that release was called more often than acquire.
	ErrRelease = errors.New("semaphore release error: returned tokens must be <= acquired tokens")
	// ErrRequestQueueFull indicates the breaker queue depth was exceeded.
	ErrRequestQueueFull = errors.New("pending request queue full")
)

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
	inFlight   int64
	totalSlots int64
	sem        *semaphore
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
	sem := newSemaphore(params.MaxConcurrency, params.InitialCapacity)
	return &Breaker{
		totalSlots: int64(params.QueueDepth + params.MaxConcurrency),
		sem:        sem,
	}
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
		cur := atomic.LoadInt64(&b.inFlight)
		if cur == b.totalSlots {
			return false
		}
		if atomic.CompareAndSwapInt64(&b.inFlight, cur, cur+1) {
			return true
		}
	}
}

// releasePending releases a slot on the pending "queue".
func (b *Breaker) releasePending() {
	atomic.AddInt64(&b.inFlight, -1)
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
	return func() {
		b.sem.release()
		b.releasePending()
	}, true
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
	return int(atomic.LoadInt64(&b.inFlight))
}

// UpdateConcurrency updates the maximum number of in-flight requests.
func (b *Breaker) UpdateConcurrency(size int) error {
	return b.sem.updateCapacity(size)
}

// Capacity returns the number of allowed in-flight requests on this breaker.
func (b *Breaker) Capacity() int {
	return b.sem.Capacity()
}

// newSemaphore creates a semaphore with the desired maximal and initial capacity.
// Maximal capacity is the size of the buffered channel, it defines maximum number of tokens
// in the rotation. Attempting to add more capacity then the max will result in error.
// Initial capacity is the initial number of free tokens.
func newSemaphore(maxCapacity, initialCapacity int) *semaphore {
	queue := make(chan struct{}, maxCapacity)
	sem := &semaphore{queue: queue}
	if initialCapacity > 0 {
		sem.updateCapacity(initialCapacity)
	}
	return sem
}

// semaphore is an implementation of a semaphore based on atomics and Go channels.
//
// The atomically handled `slots` field defines the free capacity of the semaphore.
// As long as it's positive, new tokens can get acquired immediately. When it becomes
// negative, new work will be delayed using the queue channel. These workers will be
// unblocked as other workers release their tokens.
type semaphore struct {
	slots int64
	queue chan struct{}

	mux sync.RWMutex
	cap int64
}

// tryAcquire receives the token from the semaphore if there's one
// otherwise an error is returned.
func (s *semaphore) tryAcquire() bool {
	// We only want this to succeed if there's actual capacity and fail immediately if
	// the workload would have to queue.
	for {
		cur := atomic.LoadInt64(&s.slots)
		if cur == 0 {
			return false
		}
		if atomic.CompareAndSwapInt64(&s.slots, cur, cur-1) {
			return true
		}
	}
}

// acquire receives the token from the semaphore, potentially blocking.
func (s *semaphore) acquire(ctx context.Context) error {
	if atomic.AddInt64(&s.slots, -1) >= 0 {
		return nil
	}

	select {
	case <-s.queue:
		return nil
	case <-ctx.Done():
		// Return the token immediately.
		atomic.AddInt64(&s.slots, 1)
		return ctx.Err()
	}
}

// release potentially puts the token back to the queue.
// If the semaphore capacity was reduced in between and is not yet reflected,
// we remove the tokens from the rotation instead of returning them back.
func (s *semaphore) release() error {
	if atomic.AddInt64(&s.slots, 1) > 0 {
		return nil
	}

	// We make sure releasing a token is always non-blocking.
	select {
	case s.queue <- struct{}{}:
		return nil
	default:
		// This only happens if release is called more often than acquire.
		// Return the token immediately.
		atomic.AddInt64(&s.slots, -1)
		return ErrRelease
	}
}

// updateCapacity updates the capacity of the semaphore to the desired
// size.
func (s *semaphore) updateCapacity(size int) error {
	// cap(s.queue) is equal to the maxConcurrency of the semaphore.
	if size < 0 || size > cap(s.queue) {
		return ErrUpdateCapacity
	}

	s.mux.Lock()
	defer s.mux.Unlock()

	size64 := int64(size)
	atomic.AddInt64(&s.slots, size64-s.cap)
	s.cap = size64

	return nil
}

// Capacity is the capacity.
func (s *semaphore) Capacity() int {
	s.mux.RLock()
	defer s.mux.RUnlock()

	return int(s.cap)
}
