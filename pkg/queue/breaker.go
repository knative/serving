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
		defer func() { b.sem.Put(); <-b.pendingRequests }()
		// Do the thing.
		thunk()
		// Report success
		return true
	}
}

// NewSemaphore creates a semaphore with the desired maximal capacity and initial capacity
func NewSemaphore(maxCapacity, initialCapacity int32) *Semaphore {
	if initialCapacity < 0 || initialCapacity > maxCapacity {
		panic(fmt.Sprintf("Initial capacity must be between 0 and maximal capacity. Got %v.", initialCapacity))
	}
	waiters := make(chan token, maxCapacity)
	reducers := make(chan token, maxCapacity)
	mainQueue := make(chan token, maxCapacity)
	sem := Semaphore{waitersQueue: waiters, reducersQueue: reducers, mainQueue: mainQueue}
	if initialCapacity > 0 {
		//for i := int32(0); i < initialCapacity; i++ {
		//	sem.AddCapacity()
		//}
		sem.AddCapacity(initialCapacity)
	}
	return &sem
}

// Semaphore is an implementation of a semaphore based on Go channels
type Semaphore struct {
	mux           sync.Mutex
	waitersQueue  chan token
	reducersQueue chan token
	mainQueue     chan token
	available     int32
	capacity      int32
	waiters       int32
	reducers      int32
	token         token
}

// Get receives the token from the semaphore, potentially blocking
func (s *Semaphore) Get() {
	s.get(&s.waitersQueue, &s.waiters, false)
}

// Put releases the token to one of the queues
// The operation is potentially blocking when one of the queues is full
func (s *Semaphore) Put() {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.put()
}

// ReduceCapacity removes the tokens from the rotation
// the operation is blocking until all requested tokens are available
func (s *Semaphore) ReduceCapacity(size int32) {
	for i := int32(0); i < size; i++ {
		s.get(&s.reducersQueue, &s.reducers, true)
	}
}

// AddCapacity adds capacity to the semaphore
func (s *Semaphore) AddCapacity(size int32) {
	s.mux.Lock()
	defer s.mux.Unlock()
	for i := int32(0); i < size; i++ {
		s.put()
		s.capacity++
	}
}

func (s *Semaphore) get(queue *chan token, counter *int32, cap bool) {
	s.mux.Lock()
	if s.available > 0 {
		s.available--
		if cap {
			s.capacity--
		}
		*queue <- <-s.mainQueue
	} else {
		*counter++
	}
	s.mux.Unlock()
	<-*queue
}

func (s *Semaphore) put() {
	if s.reducers > 0 {
		s.reducers--
		s.reducersQueue <- s.token
	} else if s.waiters > 0 {
		s.waiters--
		s.waitersQueue <- s.token
	} else {
		// no one needs the token now, store it for future use
		s.mainQueue <- s.token
		s.available++
	}
}
