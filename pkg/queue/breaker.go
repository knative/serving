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

import "fmt"

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
func NewBreaker(queueDepth, maxConcurrency int32, initialCapacity int32) *Breaker {
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
		b.sem.Get()
		// Defer releasing capacity in the active and pending request queue.
		defer func() { b.sem.Put(1); <-b.pendingRequests }()
		// Do the thing.
		thunk()
		// Report success
		return true
	}
}

// NewSemaphore creates a new semaphore of a given size with initial capacity
func NewSemaphore(size int32, initialCapacity int32) *Semaphore {
	ch := make(chan token, size)
	if initialCapacity < 0 || initialCapacity > size {
		panic(fmt.Sprintf("Initial capacity must be between 0 and size. Got %v.", initialCapacity))
	}
	sem := Semaphore{activeRequests: ch}
	if initialCapacity > 0 {
		for i := int32(0); i < initialCapacity; i++ {
			sem.activeRequests <- sem.token
		}
	}
	return &sem
}

// Semaphore is an implementation of a semaphore based on Go channels
type Semaphore struct {
	activeRequests chan token
	token          token
}

// Get acquires the lock from the semaphore, potentially blocking
func (s *Semaphore) Get() {
	<-s.activeRequests
}

// Put the lock back to the semaphore, blocks if the activeRequests buffer is full
func (s *Semaphore) Put(size int32) {
	for i := int32(0); i < size; i++ {
		s.activeRequests <- s.token
	}
}
