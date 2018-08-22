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
	"reflect"
	"runtime"
	"sync"
	"testing"
)

type request struct {
	lock     *sync.Mutex
	accepted chan bool
}

func TestBreakerOverload(t *testing.T) {
	b := NewBreaker(1, 1)             // Breaker capacity = 2
	want := []bool{true, true, false} // Only first two requests will be processed

	locks := b.concurrentRequests(3)
	unlockAll(locks)

	got := accepted(locks)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("Wanted %v. Got %v.", want, got)
	}
}

func TestBreakerNoOverload(t *testing.T) {
	b := NewBreaker(1, 1)                  // Breaker capacity = 2
	want := []bool{true, true, true, true} // Only two requests will be in flight at a time

	locks := make([]request, 4)
	locks[0] = b.concurrentRequest()
	locks[1] = b.concurrentRequest()
	unlock(locks[0])
	locks[2] = b.concurrentRequest()
	unlock(locks[1])
	locks[3] = b.concurrentRequest()
	unlockAll(locks[2:])
	got := accepted(locks)

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("Wanted %v. Got %v.", want, got)
	}
}

func TestBreakerRecover(t *testing.T) {
	b := NewBreaker(1, 1)                                // Breaker capacity = 2
	want := []bool{true, true, false, false, true, true} // Shedding will stop when capacity opens up

	locks := b.concurrentRequests(4)
	unlockAll(locks)
	// Breaker recovers
	moreLocks := b.concurrentRequests(2)
	unlockAll(moreLocks)

	got := accepted(append(locks, moreLocks...))
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("Wanted %v. Got %v.", want, got)
	}
}

func TestBreakerLargeCapacityRecover(t *testing.T) {
	b := NewBreaker(5, 45)    // Breaker capacity = 50
	want := make([]bool, 150) // Process 150 requests
	for i := 0; i < 50; i++ {
		want[i] = true // First 50 will fill the breaker capacity
	}
	for i := 50; i < 100; i++ {
		want[i] = false // The next 50 will be shed
	}
	for i := 100; i < 150; i++ {
		want[i] = true // The next 50 will be processed as capacity opens up
	}

	// Send 100 requests
	locks := b.concurrentRequests(100)
	// Process one request and send one request, 50 times
	for i := 100; i < 150; i++ {
		// Open capacity
		unlock(locks[i-100])
		// Add another request
		locks = append(locks, b.concurrentRequest())
	}
	unlockAll(locks[50:])

	got := accepted(locks)
	// Check the first few suceeded
	if !reflect.DeepEqual(want[:10], got[:10]) {
		t.Fatalf("Wanted %v. Got %v.", want, got)
	}
	// Check the breaker tripped
	if !reflect.DeepEqual(want[60:70], got[60:70]) {
		t.Fatalf("Wanted %v. Got %v.", want, got)
	}
	// Check the breaker reset
	if !reflect.DeepEqual(want[len(want)-10:], got[len(got)-10:]) {
		t.Fatalf("Wanted\n%v.\nGot\n%v.", want, got)
	}
}

func TestUnlimitedBreaker(t *testing.T) {
	b := NewBreaker(1, 0)
	requests := b.concurrentRequests(1000)
	unlockAll(requests)
	for i, ok := range accepted(requests) {
		if !ok {
			t.Fatalf("Expected request %d to be successful, but it failed.", i)
		}
	}
}

// Attempts to perform a concurrent request against the specified breaker.
func (b *Breaker) concurrentRequest() request {

	// There is a brief window between when capacity is released and
	// when it becomes available to the next request.  We yield here
	// to reduce the likelihood that we hit that edge case.  E.g.
	// without yielding `go test ./pkg/queue/breaker.* -count 10000`
	// will fail about 3 runs.
	runtime.Gosched()

	r := request{lock: &sync.Mutex{}, accepted: make(chan bool, 2)}
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
	for i := 0; i < n; i++ {
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
