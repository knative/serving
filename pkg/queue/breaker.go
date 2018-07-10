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
	activeRequests  chan token
}

// NewBreaker creates a Breaker with the desired queue depth and
// concurrency limit. If maxConcurrency is 0, this breaker will not
// enforce any maximum concurrency, and will simply forward all
// requests.
func NewBreaker(queueDepth, maxConcurrency int) *Breaker {
	if queueDepth <= 0 {
		panic(fmt.Sprintf("Queue depth must be greater than 0. Got %v.", queueDepth))
	}
	if maxConcurrency < 0 {
		panic(fmt.Sprintf("Max concurrency must be 0 or greater. Got %v.", maxConcurrency))
	}
	retval := &Breaker{
		// Active requests keep their pending capacity until
		// after the request has completed, to prevent hiccups
		// where there is active capacity which has not yet
		// been utilized by one of the pending requests. (This
		// manifests as flakiness in tests.)
		pendingRequests: make(chan token, queueDepth+maxConcurrency),
	}
	if maxConcurrency > 0 {
		retval.activeRequests = make(chan token, maxConcurrency)
	}
	return retval

}

// Maybe conditionally executes thunk based on the Breaker concurrency
// and queue parameters. If the concurrency limit and queue capacity are
// already consumed, Maybe returns immediately without calling thunk. If
// the thunk was executed, Maybe returns true, else false.
func (b *Breaker) Maybe(thunk func()) bool {
	if b.activeRequests == nil {
		// unlimited concurrency
		thunk()
		return true
	}
	var t token
	select {
	default:
		// Pending request queue is full.  Report failure.
		return false
	case b.pendingRequests <- t:
		// Pending request has capacity.
		// Acquire capacity in the active queue, too.
		b.activeRequests <- t
		// Defer releasing any capacity.
		defer func() { <-b.activeRequests; <-b.pendingRequests }()
		// Do the thing.
		thunk()
		// Report success
		return true
	}
}
