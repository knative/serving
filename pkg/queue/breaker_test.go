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
	"runtime"
	"sync"
	"testing"
)


type request struct {
	lock     *sync.Mutex
	accepted chan bool
}


func TestBreakerRecover(t *testing.T) {
	b := NewBreaker(1, 1)                                // Breaker capacity = 2

	locks := b.concurrentRequests(4)
	unlockAll(locks)
}

// Attempts to perform a concurrent request against the specified breaker.
func (b *Breaker) concurrentRequest() request {

	// There is a brief window between when capacity is released and
	// when it becomes available to the next request.  We yield here
	// to reduce the likelihood that we hit that edge case.  E.g.
	// without yielding `go test ./pkg/queue/breaker.* -count 10000`
	// will fail about 3 runs.
	runtime.Gosched()

	r := request{lock: &sync.Mutex{}, accepted: make(chan bool, 1)}
	r.lock.Lock()
	var start sync.WaitGroup
	start.Add(1)
	go func() {
		start.Done()
		ok := b.Maybe(func() {
			r.lock.Lock() // Will block on locked mutex.
			r.lock.Unlock()
		})
		r.accepted <- ok
	}()
	start.Wait() // Ensure that the go func has had a chance to execute.
	return r
}

// Perform n requests against the breaker, returning mutexes for each
// request which succeeded, and a slice of bools for all requests.
func (b *Breaker) concurrentRequests(n int) []request {
	requests := make([]request, n)
	for i := range requests {
		requests[i] = b.concurrentRequest()
	}
	return requests
}

func accepted(requests []request) []bool {
	got := make([]bool, len(requests))
	for i, r := range requests {
		got[i] = <-r.accepted
	}
	return got
}

func unlock(req request) {
	req.lock.Unlock()
	// Verify that function has completed
	ok := <-req.accepted
	// Requeue for next usage
	req.accepted <- ok
}

func unlockAll(requests []request) {
	for _, lc := range requests {
		unlock(lc)
	}
}
