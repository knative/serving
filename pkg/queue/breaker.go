package queue

import "fmt"

type token struct{}

type Breaker struct {
	pendingRequests chan token
	activeRequests  chan token
}

func NewBreaker(queueDepth, maxConcurrency int32) *Breaker {
	if queueDepth <= 0 {
		panic(fmt.Sprintf("Queue depth must be greater than 0. Got %v.", queueDepth))
	}
	if maxConcurrency <= 0 {
		panic(fmt.Sprintf("Max concurrency must be greater than 0. Got %v.", maxConcurrency))
	}
	return &Breaker{
		pendingRequests: make(chan token, queueDepth),
		activeRequests:  make(chan token, maxConcurrency),
	}
}

func (b *Breaker) Maybe(thunk func()) bool {
	var t token
	select {
	default:
		// Pending request queue is full.  Report failure.
		return false
	case b.pendingRequests <- t:
		// Pending request has capacity.
		// Wait for capacity in the active queue.
		b.activeRequests <- t
		// Release capacity in the pending request queue.
		<-b.pendingRequests
		// Defer releasing capacity in the active request queue.
		defer func() { <-b.activeRequests }()
		// Do the thing.
		thunk()
		// Report success
		return true
	}
}
