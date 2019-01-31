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
	"errors"
	"fmt"
	"sync"
	"go.uber.org/zap"
)

const (
	reduceCapacityError = "the capacity that is released must be <= to added capacity"
	addCapacityError    = "failed to add all capacity to the breaker"
)

// BreakerParams defines the parameters of the breaker.
type BreakerParams struct {
	QueueDepth      int32
	MaxConcurrency  int32
	InitialCapacity int32
	Logger          *zap.SugaredLogger
}

type token struct{}

// Breaker is a component that enforces a concurrency limit on the
// execution of a function. It also maintains a queue of function
// executions in excess of the concurrency limit. Function call attempts
// beyond the limit of the queue are failed immediately.
type Breaker struct {
	pendingRequests chan token
	sem             *Semaphore
}

// NewBreaker creates a Breaker with the desired queue depth,
// concurrency limit and initial capacity.
func NewBreaker(params BreakerParams) *Breaker {
	if params.QueueDepth <= 0 {
		panic(fmt.Sprintf("Queue depth must be greater than 0. Got %v.", params.QueueDepth))
	}
	if params.MaxConcurrency < 0 {
		panic(fmt.Sprintf("Max concurrency must be 0 or greater. Got %v.", params.QueueDepth))
	}
	if params.InitialCapacity < 0 || params.InitialCapacity > params.MaxConcurrency {
		panic(fmt.Sprintf("Initial capacity must be between 0 and max concurrency. Got %v.", params.InitialCapacity))
	}
	sem := NewSemaphore(params.MaxConcurrency, params.InitialCapacity, params.Logger)
	return &Breaker{
		pendingRequests: make(chan token, params.QueueDepth+params.MaxConcurrency),
		sem:             sem,
	}
}

// Maybe conditionally executes thunk based on the Breaker concurrency
// and queue parameters. If the concurrency limit and queue capacity are
// already consumed, Maybe returns immediately without calling thunk. If
// the thunk was executed, Maybe returns true, else false.
func (b *Breaker) Maybe(thunk func()) bool {

	var t token
	select {
	default:
		// Pending request queue is full.  Report failure.
		return false
	case b.pendingRequests <- t:
		// Pending request has capacity.
		// Wait for capacity in the active queue.
		b.sem.Acquire()
		// Defer releasing capacity in the active and pending request queue.
		defer func() { b.sem.Release(); <-b.pendingRequests }()
		// Do the thing.
		thunk()
		// Report success
		return true
	}
}

// UpdateConcurrency updates the maximum number of in-flight requests.
func (b *Breaker) UpdateConcurrency(size int32) (err error) {
	if size > 0 {
		err = b.sem.AddCapacity(size)
	} else {
		err = b.sem.ReduceCapacity(-size)
	}
	return err
}

// Capacity retrieves the capacity of the breaker.
func (b *Breaker) Capacity() int32 {
	return b.sem.capacity
}

// NewSemaphore creates a semaphore with the desired maximal and initial capacity.
func NewSemaphore(maxCapacity, initialCapacity int32, logger *zap.SugaredLogger) *Semaphore {
	if initialCapacity < 0 || initialCapacity > maxCapacity {
		panic(fmt.Sprintf("Initial capacity must be between 0 and maximal capacity. Got %v.", initialCapacity))
	}
	queue := make(chan token, maxCapacity)
	sem := Semaphore{queue: queue, logger: logger}
	if initialCapacity > 0 {
		sem.AddCapacity(initialCapacity)
	}
	return &sem
}

// Semaphore is an implementation of a semaphore based on Go channels.
// The presence of elements in the `queue` buffered channel correspond to available tokens.
// Hence the max number of tokens to hand out equals to the size of the channel.
// `capacity` defines the current number of tokens in the rotation.
type Semaphore struct {
	queue    chan token
	token    token
	reducers int32
	capacity int32
	logger   *zap.SugaredLogger
	mux      sync.Mutex
}

// Acquire receives the token from the semaphore, potentially blocking.
func (s *Semaphore) Acquire() {
	<-s.queue
}

// Release potentially puts the token back to the queue.
// If the semaphore capacity was reduced in between and is not yet reflected,
// we remove the tokens from the rotation instead of returning them back.
func (s *Semaphore) Release() {
	s.mux.Lock()
	defer s.mux.Unlock()
	if s.reducers > 0 {
		s.capacity -= s.reducers
		s.reducers--
	} else {
		// We want to make sure releasing a token is always non-blocking.
		select {
		case s.queue <- s.token:
		default:
			// this should never happen
			s.logger.Error("Semaphore release error: returned tokens must be <= acquired tokens")
		}
	}
}

// AddCapacity increases the number of tokens in the rotation.
// Increases only if the size is positive, does nothing otherwise.
// An error is returned if not all capacity could be added to the semaphore.
// This error could happen when the number of tokens exceeds the semaphore's queue size.
func (s *Semaphore) AddCapacity(size int32) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	for i := int32(0); i < size; i++ {
		select {
		case s.queue <- s.token:
			s.capacity++
		default:
			return errors.New(addCapacityError)
		}
	}
	return nil
}

// ReduceCapacity removes tokens from the rotation.
// It tries to acquire as many tokens as possible, if there are not enough tokens in the queue,
// it postpones the operation for the future by increasing the `reducers` counter.
func (s *Semaphore) ReduceCapacity(size int32) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	if size > s.capacity {
		return errors.New(reduceCapacityError)
	}
	for i := int32(0); i < size; i++ {
		select {
		case <-s.queue:
			s.capacity--
		default:
			s.reducers++
		}
	}
	return nil
}
