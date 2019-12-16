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
	pendingRequests chan struct{}
	sem             *semaphore
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
		pendingRequests: make(chan struct{}, params.QueueDepth+params.MaxConcurrency),
		sem:             sem,
	}
}

// Reserve reserves an execution slot in the breaker, to permit
// richer semantics in the caller.
// The caller on success must execute the callback when done with work.
func (b *Breaker) Reserve(ctx context.Context) (func(), bool) {
	select {
	default:
		// Pending request queue is full.  Report failure.
		return nil, false
	case b.pendingRequests <- struct{}{}:
		// Pending request has capacity, reserve a slot, if there's one
		// available.
		if !b.sem.tryAcquire(ctx) {
			<-b.pendingRequests
			return nil, false
		}
		return func() {
			b.sem.release()
			<-b.pendingRequests
		}, true
	}
}

// Maybe conditionally executes thunk based on the Breaker concurrency
// and queue parameters. If the concurrency limit and queue capacity are
// already consumed, Maybe returns immediately without calling thunk. If
// the thunk was executed, Maybe returns true, else false.
func (b *Breaker) Maybe(ctx context.Context, thunk func()) error {
	select {
	default:
		// Pending request queue is full.  Report failure.
		return ErrRequestQueueFull
	case b.pendingRequests <- struct{}{}:
		// Pending request has capacity.
		// Defer releasing pending request queue.
		defer func() {
			<-b.pendingRequests
		}()

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
}

// InFlight returns the number of requests currently in flight in this breaker.
func (b *Breaker) InFlight() int {
	return len(b.pendingRequests)
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

// semaphore is an implementation of a semaphore based on Go channels.
// The presence of elements in the `queue` buffered channel correspond to available tokens.
// Hence the max number of tokens to hand out equals to the size of the channel.
// `capacity` defines the current number of tokens in the rotation.
type semaphore struct {
	queue    chan struct{}
	reducers int
	capacity int
	mux      sync.RWMutex
}

// tryAcquire receives the token from the semaphore if there's one
// otherwise an error is returned.
func (s *semaphore) tryAcquire(ctx context.Context) bool {
	select {
	case <-s.queue:
		return true
	default:
		return false
	}
}

// acquire receives the token from the semaphore, potentially blocking.
func (s *semaphore) acquire(ctx context.Context) error {
	select {
	case <-s.queue:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release potentially puts the token back to the queue.
// If the semaphore capacity was reduced in between and is not yet reflected,
// we remove the tokens from the rotation instead of returning them back.
func (s *semaphore) release() error {
	s.mux.Lock()
	defer s.mux.Unlock()

	if s.reducers > 0 {
		s.capacity--
		s.reducers--
		return nil
	}

	// We want to make sure releasing a token is always non-blocking.
	select {
	case s.queue <- struct{}{}:
		return nil
	default:
		// This only happens if release is called more often than acquire.
		return ErrRelease
	}
}

// updateCapacity updates the capacity of the semaphore to the desired
// size.
func (s *semaphore) updateCapacity(size int) error {
	if size < 0 || size > cap(s.queue) {
		return ErrUpdateCapacity
	}

	s.mux.Lock()
	defer s.mux.Unlock()

	if s.effectiveCapacity() == size {
		return nil
	}

	// Add capacity until we reach size, potentially consuming
	// outstanding reducers first.
	for s.effectiveCapacity() < size {
		if s.reducers > 0 {
			s.reducers--
		} else {
			select {
			case s.queue <- struct{}{}:
				s.capacity++
			default:
				// This indicates that we're operating close to
				// MaxCapacity and returned more tokens than we
				// acquired.
				return ErrUpdateCapacity
			}
		}
	}

	// Reduce capacity until we reach size, potentially adding
	// new reducers if the queue channel is empty because of
	// requests in-flight.
	for s.effectiveCapacity() > size {
		select {
		case <-s.queue:
			s.capacity--
		default:
			s.reducers++
		}
	}

	return nil
}

// effectiveCapacity is the capacity with reducers taken into account.
// `mux` must be held to call it.
func (s *semaphore) effectiveCapacity() int {
	return s.capacity - s.reducers
}

// Capacity is the effective capacity after taking reducers into account.
func (s *semaphore) Capacity() int {
	s.mux.RLock()
	defer s.mux.RUnlock()

	return s.effectiveCapacity()
}
