package kbuffer

import (
	"sync"
	"time"
)

func NewThrottler(requests []interface{}, concurrency int, endpoints func(id revisionID) int, processor func([]interface{}), ticker *Ticker) *Throttler {
	return &Throttler{requests: requests, concurrency: concurrency, endpoints: endpoints, processor: processor, ticker: ticker}
}

type Throttler struct {
	requests    []interface{}
	concurrency int
	endpoints   func(id revisionID) int
	processor   func([]interface{})
	ticker      *Ticker
	stopChan    chan struct{}
	stopped     bool
	mux         sync.Mutex
}

func (t *Throttler) requestsToProcess(id revisionID) int{
	requests := t.endpoints(id) * t.concurrency
	if requests > cap(t.requests){
		requests = cap(t.requests)
	}
	return requests
}

func (t *Throttler) process(id revisionID) {
	defer t.mux.Unlock()
	t.mux.Lock()
	sliceSize := t.requestsToProcess(id)
	requests := t.requests[:sliceSize]
	t.requests = t.requests[sliceSize:]
	// start processing in a new go-routine to release the mutex as fast as possible
	go t.processor(requests)
}

func (t *Throttler) Run(id revisionID) {
	defer func(){
		t.stopped = true
	}()
	for {
		<-t.ticker.tickChan
		if (cap(t.requests)) > 0 {
			go t.process(id)
		} else {
			t.ticker.stopChan <- struct{}{}
			return
		}
	}
}

func NewTicker(period time.Duration) *Ticker {
	stopChan := make(chan struct{})
	tickChan := make(chan struct{})
	return &Ticker{period: period, tickChan: tickChan, stopChan: stopChan}
}

type Ticker struct {
	period   time.Duration
	tickChan chan struct{}
	stopChan chan struct{}
	stopped  bool
}

// Tick until a message from stopChan is received or the number of ticks is exhausted
func (tk *Ticker) Tick(ticks ...int) {
	defer func() {
		tk.stopped = true
	}()
	var ticked int
	var tick = func() {
		tk.tickChan <- struct{}{}
		time.Sleep(tk.period)
	}
	for {
		select {
		case <-tk.stopChan:
			return
		default:
			if ticks == nil {
				tick()
			} else {
				if ticked < ticks[0] {
					tick()
					ticked++
				} else {
					return
				}
			}
		}
	}
}
