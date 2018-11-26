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
)

type token struct{}

// Breaker is a component that enforces a concurrency limit on the
// execution of a function. It also maintains a queue of function
// executions in excess of the concurrency limit. Function call attempts
// beyond the limit of the queue are failed immediately.
type Breaker struct {
	maxConcurrency  int32
	pendingRequests chan token
	activeRequests  chan token
	Sem             *Semaphore
}

// NewBreaker creates a Breaker with the desired queue depth and
// concurrency limit.
func NewBreaker(queueDepth, maxConcurrency int32, empty bool) *Breaker {
	if queueDepth <= 0 {
		panic(fmt.Sprintf("Queue depth must be greater than 0. Got %v.", queueDepth))
	}
	if maxConcurrency < 0 {
		panic(fmt.Sprintf("Max concurrency must be 0 or greater. Got %v.", maxConcurrency))
	}

	sem := NewSemaphore(maxConcurrency, empty)
	return &Breaker{
		maxConcurrency:  maxConcurrency,
		pendingRequests: make(chan token, queueDepth+maxConcurrency),
		activeRequests:  make(chan token, maxConcurrency),
		Sem:             sem,
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
		//b.activeRequests <- t
		b.Sem.Get()
		// Defer releasing capacity in the active and pending request queue.
		//defer func() { <-b.activeRequests; <-b.pendingRequests }()
		defer func() { b.Sem.Put(1); <-b.pendingRequests }()
		// Do the thing.
		thunk()
		// Report success
		return true
	}
}

func (b *Breaker) Total() int {
	return cap(b.pendingRequests)
}

func NewSemaphore(size int32, empty bool) *Semaphore {
	ch := make(chan token, size)
	return &Semaphore{activeRequests: ch, empty: empty}
}

type Semaphore struct {
	activeRequests chan token
	empty          bool
}

func (s *Semaphore) Get() {
	var t token
	if s.empty {
		<-s.activeRequests
	} else {
		s.activeRequests <- t
	}
}

func (s *Semaphore) Put(size int32) {
	var t token
	for i := int32(0); i < size; i++ {
		if s.empty {
			s.activeRequests <- t
		} else {
			<-s.activeRequests
		}
	}
}
