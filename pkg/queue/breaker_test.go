package queue

import (
	"reflect"
	"runtime"
	"testing"
)

func TestBreakerOverload(t *testing.T) {
	b := NewBreaker(1, 1)             // Breaker capacity = 2
	want := []bool{true, true, false} // Only first two requests will be processed

	r1, g1 := b.concurrentRequest()
	r2, g2 := b.concurrentRequest()
	r3, g3 := b.concurrentRequest() // Will be shed
	done(r1)
	done(r2)
	done(r3)
	got := []bool{<-g1, <-g2, <-g3}

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("Wanted %v. Got %v.", want, got)
	}
}

func TestBreakerNoOverload(t *testing.T) {
	b := NewBreaker(1, 1)                  // Breaker capacity = 2
	want := []bool{true, true, true, true} // Only two requests will be in flight at a time

	r1, g1 := b.concurrentRequest()
	r2, g2 := b.concurrentRequest()
	done(r1)
	r3, g3 := b.concurrentRequest()
	done(r2)
	r4, g4 := b.concurrentRequest()
	done(r3)
	done(r4)
	got := []bool{<-g1, <-g2, <-g3, <-g4}

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("Wanted %v. Got %v.", want, got)
	}
}

func TestBreakerRecover(t *testing.T) {
	b := NewBreaker(1, 1)                                // Breaker capacity = 2
	want := []bool{true, true, false, false, true, true} // Shedding will stop when capacity opens up

	r1, g1 := b.concurrentRequest()
	r2, g2 := b.concurrentRequest()
	_, g3 := b.concurrentRequest() // Will be shed
	_, g4 := b.concurrentRequest() // Will be shed
	done(r1)
	done(r2)
	// Breaker recovers
	r5, g5 := b.concurrentRequest()
	r6, g6 := b.concurrentRequest()
	done(r5)
	done(r6)
	got := []bool{<-g1, <-g2, <-g3, <-g4, <-g5, <-g6}

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

	releases := make([]chan struct{}, 0)
	gots := make([]chan bool, 0)
	// Send 100 requests
	for i := 0; i < 100; i++ {
		r, g := b.concurrentRequest()
		releases = append(releases, r)
		gots = append(gots, g)
	}
	// Process one request and send one request, 50 times
	for i := 100; i < 150; i++ {
		// Open capacity
		done(releases[0])
		releases = releases[1:]
		// Add another request
		r, g := b.concurrentRequest()
		releases = append(releases, r)
		gots = append(gots, g)
	}
	// Process remaining requests
	for _, r := range releases {
		done(r)
	}
	// Collect the results
	got := make([]bool, len(gots))
	for i, g := range gots {
		got[i] = <-g
	}

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("Wanted %v. Got %v.", want, got)
	}
}

func (b *Breaker) concurrentRequest() (chan struct{}, chan bool) {
	release := make(chan struct{})
	thunk := func() {
		_, _ = <-release
	}
	result := make(chan bool)
	go func() {
		result <- b.Maybe(thunk)
	}()
	runtime.Gosched()
	return release, result
}

func done(release chan struct{}) {
	close(release)
	// Allow enough time for the two goroutines involved to shuffle
	// the request out of active and another request out of the
	// queue.
	runtime.Gosched()
	runtime.Gosched()
}
