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
	"sync"
	"testing"
	"time"

	"errors"

	"k8s.io/apimachinery/pkg/util/wait"
)

// semAcquireTimeout is a timeout for tests that try to acquire
// a token of a semaphore.
const semAcquireTimeout = 10 * time.Second

// semNoChangeTimeout is some additional wait time after a number
// of acquires is reached to assert that no more acquires get through.
const semNoChangeTimeout = 50 * time.Millisecond

type request struct {
	lock     *sync.Mutex
	accepted chan bool
}

func (r *request) wait() {
	ok := <-r.accepted
	// Requeue for next usage
	r.accepted <- ok
}

func TestBreakerOverload(t *testing.T) {
	b := NewBreaker(1, 1, 1)          // Breaker capacity = 2
	want := []bool{true, true, false} // Only first two requests will be processed

	locks := b.concurrentRequests(3)

	unlockAll(locks)

	assertEqual(want, accepted(locks), t)
}

func TestBreakerOverloadWithEmptySemaphore(t *testing.T) {
	b := NewBreaker(1, 1, 0)          // Breaker capacity = 2
	want := []bool{true, true, false} // Only first two requests are processed

	b.sem.Release()
	locks := b.concurrentRequests(3)

	unlockAll(locks)

	assertEqual(want, accepted(locks), t)
}

func TestBreakerNoOverload(t *testing.T) {
	b := NewBreaker(1, 1, 1)               // Breaker capacity = 2
	want := []bool{true, true, true, true} // Only two requests will be in flight at a time
	locks := make([]request, 4)
	locks[0] = b.concurrentRequest()
	locks[1] = b.concurrentRequest()
	unlock(locks[0])
	locks[2] = b.concurrentRequest()
	unlock(locks[1])
	locks[3] = b.concurrentRequest()
	unlockAll(locks[2:])

	assertEqual(want, accepted(locks), t)
}

func TestBreakerRecover(t *testing.T) {
	b := NewBreaker(1, 1, 1)                             // Breaker capacity = 2
	want := []bool{true, true, false, false, true, true} // Shedding will stop when capacity opens up

	locks := b.concurrentRequests(4)
	unlockAll(locks)
	// Breaker recovers
	moreLocks := b.concurrentRequests(2)
	unlockAll(moreLocks)

	assertEqual(want, accepted(append(locks, moreLocks...)), t)
}

func TestBreakerLargeCapacityRecover(t *testing.T) {
	b := NewBreaker(5, 45, 45) // Breaker capacity = 50
	want := make([]bool, 150)  // Process 150 requests
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

	assertEqual(want, accepted(locks), t)
}

// Test empty semaphore, token cannot be acquired
func TestSemaphore_Get_HasNoCapacity(t *testing.T) {
	gotChan := make(chan struct{}, 1)

	sem := NewSemaphore(1, 0)
	tryAcquire(sem, gotChan)

	select {
	case <-gotChan:
		t.Error("Token was acquired but shouldn't have been")
	case <-time.After(20 * time.Millisecond):
		// Test succeeds, semaphore didn't change in configured time
	}
}

// Test empty semaphore, add capacity, token can be acquired
func TestSemaphore_Get_HasCapacity(t *testing.T) {
	gotChan := make(chan struct{}, 1)

	sem := NewSemaphore(1, 0)
	tryAcquire(sem, gotChan)
	sem.Release() // Allows 1 acquire

	assertAcquired(1, gotChan, t)
}

//Test all put items can be consumed
func TestSemaphore_Put(t *testing.T) {
	gotChan := make(chan struct{}, 1)

	requests := 3
	sem := NewSemaphore(2, 0)
	for i := 0; i < requests; i++ {
		tryAcquire(sem, gotChan)
	}
	sem.Release()
	sem.Release() // Allows 2 acquires

	assertAcquired(2, gotChan, t)
}

func TestSemaphore_AddCapacity(t *testing.T) {
	sem := NewSemaphore(2, 1)
	assertEqual(int32(1), sem.capacity, t)
	sem.Acquire()
	sem.AddCapacity(2)
	assertEqual(int32(3), sem.capacity, t)
}

// Test the case when we add more capacity then the number of waiting reducers
func TestSemaphore_AddCapacityLessThenReducers(t *testing.T) {
	sem := NewSemaphore(2, 2)
	sem.Acquire()
	sem.Acquire()
	sem.ReduceCapacity(2)
	assertEqual(int32(2), sem.reducers, t)
	sem.AddCapacity(3)
	assertEqual(int32(0), sem.reducers, t)
}

func TestSemaphore_ReduceCapacity(t *testing.T) {
	want := int32(0)
	sem := NewSemaphore(1, 0)
	sem.AddCapacity(int32(1))
	sem.ReduceCapacity(1)
	assertEqual(want, sem.capacity, t)
}

func TestSemaphore_ReduceCapacity_NoCapacity(t *testing.T) {
	sem := NewSemaphore(1, 1)
	sem.Acquire()
	sem.ReduceCapacity(1)
	assertEqual(int32(1), sem.reducers, t)
	sem.Release()
	assertEqual(int32(0), sem.reducers, t)
	assertEqual(int32(0), sem.capacity, t)
}

func TestSemaphore_ReduceCapacity_OutOfBound(t *testing.T) {
	sem := NewSemaphore(1, 1)
	sem.Acquire()
	err := sem.ReduceCapacity(2)
	assertEqual(err, errors.New("the capacity that is released must be <= to added capacity"), t)
}

func TestSemaphore_WrongInitialCapacity(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("The code did not panic")
		}
	}()
	_ = NewSemaphore(1, 2)
}

// Attempts to perform a concurrent request against the specified breaker.
// Will wait for request to either be performed, enqueued or rejected.
func (b *Breaker) concurrentRequest() request {
	r := request{lock: &sync.Mutex{}, accepted: make(chan bool, 1)}
	r.lock.Lock()

	if len(b.sem.queue) > 0 {
		// Expect request to be performed
		defer waitForQueue(b.sem.queue, len(b.sem.queue)-1)
	} else if len(b.pendingRequests) < cap(b.pendingRequests) {
		// Expect request to be queued
		defer waitForQueue(b.pendingRequests, len(b.pendingRequests)+1)
	} else {
		// Expect request to be rejected
		defer r.wait()
	}

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

func waitForQueue(queue chan token, size int) {
	err := wait.PollImmediate(1*time.Millisecond, 100*time.Millisecond, func() (bool, error) {
		return len(queue) == size, nil
	})
	if err != nil {
		panic("timed out waiting for queue")
	}
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
	req.wait()
}

func unlockAll(requests []request) {
	for _, lc := range requests {
		unlock(lc)
	}
}

func assertEqual(want, got interface{}, t *testing.T) {
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("Wanted %v. Got %v.", want, got)
	}
}

func tryAcquire(sem *Semaphore, gotChan chan struct{}) {
	go func() {
		// blocking until someone puts the token into the semaphore
		sem.Acquire()
		gotChan <- struct{}{}
	}()
}

// assertAcquired waits for gotChan to contain the wanted number of elements.
// After these elements have arrived, we wait for a little longer to see if
// unexpected elements arrive.
func assertAcquired(want int, gotChan chan struct{}, t *testing.T) {
	for i := 0; i < want; i++ {
		select {
		case <-gotChan:
			// Successfully acquired a token.
		case <-time.After(semAcquireTimeout):
			t.Error("Was not able to acquire token before timeout")
		}
	}

	select {
	case <-gotChan:
		t.Errorf("Got more acquires than wanted, want = %d, got at least %d", want, want+1)
	case <-time.After(semNoChangeTimeout):
		// No change happened, success.
	}
}
