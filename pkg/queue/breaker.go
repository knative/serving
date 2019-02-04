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
	"fmt"
	"sync"
	"go.uber.org/zap"
	"errors"
)

var (
	ErrAddCapacity    = errors.New("failed to add all capacity to the breaker")
	ErrReduceCapacity = errors.New("the capacity that is released must be <= to added capacity")
	ErrRelease        = errors.New("semaphore release error: returned tokens must be <= acquired tokens")
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
	logger          *zap.SugaredLogger
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
	sem := NewSemaphore(params.MaxConcurrency, params.InitialCapacity)
	return &Breaker{
		pendingRequests: make(chan token, params.QueueDepth+params.MaxConcurrency),
		sem:             sem,
		logger:          params.Logger,
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
		defer func() {
			if err := b.sem.Release(); err != nil {
				b.logger.Errorw("Error while releasing a semaphore:", zap.Error(err))
			}
			<-b.pendingRequests
		}()
		// Do the thing.
		thunk()
		// Report success
		return true
	}
}

// UpdateConcurrency updates the maximum number of in-flight requests.
func (b *Breaker) UpdateConcurrency(size int32) error {
	if size > 0 {
		return b.sem.AddCapacity(size)
	}
	return b.sem.ReduceCapacity(-size)
}

// Capacity retrieves the capacity of the breaker.
func (b *Breaker) Capacity() int32 {
	return b.sem.capacity
}

// NewSemaphore creates a semaphore with the desired maximal and initial capacity.
// Maximal capacity is the size of the buffered channel, it defines maximum number of tokens
// in the rotation. Attempting to add more capacity then the max will result in error.
// Initial capacity is the initial number of free tokens.
func NewSemaphore(maxCapacity, initialCapacity int32) *Semaphore {
	if initialCapacity < 0 || initialCapacity > maxCapacity {
		panic(fmt.Sprintf("Initial capacity must be between 0 and maximal capacity. Got %v.", initialCapacity))
	}
	queue := make(chan token, maxCapacity)
	sem := Semaphore{queue: queue}
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
	mux      sync.Mutex
}

// Acquire receives the token from the semaphore, potentially blocking.
func (s *Semaphore) Acquire() {
	<-s.queue
}

// Release potentially puts the token back to the queue.
// If the semaphore capacity was reduced in between and is not yet reflected,
// we remove the tokens from the rotation instead of returning them back.
func (s *Semaphore) Release() error {
	s.mux.Lock()
	defer s.mux.Unlock()
	if s.reducers > 0 {
		s.capacity--
		s.reducers--
	} else {
		// We want to make sure releasing a token is always non-blocking.
		select {
		case s.queue <- s.token:
		default:
			// This should never happen.
			return ErrRelease
		}
	}
	return nil
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
			return ErrAddCapacity
		}
	}
	return nil
}

// ReduceCapacity removes tokens from the rotation.
// It tries to acquire as many tokens as possible. If there are not enough tokens in the queue,
// it postpones the operation for the future by increasing the `reducers` counter.
// We return an error when attempting to reduce more capacity than was originally added.
func (s *Semaphore) ReduceCapacity(size int32) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	if size+s.reducers > s.capacity {
		return ErrReduceCapacity
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
