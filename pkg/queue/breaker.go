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
)

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
// concurrency limit and initial capacity
func NewBreaker(queueDepth, maxConcurrency, initialCapacity int32) *Breaker {
	if queueDepth <= 0 {
		panic(fmt.Sprintf("Queue depth must be greater than 0. Got %v.", queueDepth))
	}
	if maxConcurrency < 0 {
		panic(fmt.Sprintf("Max concurrency must be 0 or greater. Got %v.", maxConcurrency))
	}
	if initialCapacity < 0 || initialCapacity > maxConcurrency {
		panic(fmt.Sprintf("Initial capacity must be between 0 and max concurrency. Got %v.", initialCapacity))
	}
	sem := NewSemaphore(maxConcurrency, initialCapacity)
	return &Breaker{
		pendingRequests: make(chan token, queueDepth+maxConcurrency),
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

// NewSemaphore creates a semaphore with the desired maximal and initial capacity
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

// Semaphore is an implementation of a semaphore based on Go channels
// The number of available tokens is the number of elements in the buffered channel
type Semaphore struct {
	queue    chan token
	token    token
	reducers int32
	capacity int32
	mux      sync.Mutex
}

// Acquire receives the token from the semaphore, potentially blocking
func (s *Semaphore) Acquire() {
	<-s.queue
}

// Release releases the token to the queue
// The operation is potentially blocking when the queue is full
func (s *Semaphore) Release() {
	s.AddCapacity(1)
}

// ReduceCapacity removes tokens from the rotation
// It tries to acquire as many tokens as possible, if there are not enough tokens in the queue,
// it postpones the operation for the future by increasing the `reducers` counter
func (s *Semaphore) ReduceCapacity(size int32) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	if size > s.capacity {
		return errors.New("the capacity that is released must be <= to added capacity")
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

// AddCapacity conditionally adds capacity to the semaphore
// If there are tokens that must be reduced, release them first
// Otherwise, add tokens to the queue
func (s *Semaphore) AddCapacity(size int32) {
	var leftToAdd int32
	s.mux.Lock()
	defer s.mux.Unlock()
	if s.reducers > 0 {
		// do not allow reducers to be negative
		if s.reducers >= size {
			s.reducers -= size
			s.capacity -= size
		} else {
			leftToAdd = size - s.reducers
			s.reducers = 0
		}
	} else {
		leftToAdd = size
	}
	if leftToAdd > 0 {
		for i := int32(0); i < leftToAdd; i++ {
			s.queue <- s.token
			s.capacity++
		}
	}
}
