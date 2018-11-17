package kbuffer

import (
	"sync"
	"time"
	"fmt"
)

func NewThrottler(concurrency int, endpoints func(id revisionID, postfix ...string) (int,bool), processor func([]chan ActivationResult), ticker *Ticker) *Throttler {
	return &Throttler{concurrency: concurrency, endpoints: endpoints, processor: processor, ticker: ticker}
}

type Throttler struct {
	concurrency int
	endpoints   func(id revisionID, postfix ...string) (int,bool)
	processor   func([]chan ActivationResult)
	requests []chan ActivationResult
	ticker      *Ticker
	stopChan    chan struct{}
	stopped     bool
	mux         sync.Mutex
}

func (t *Throttler) requestsToProcess(id revisionID) int{
	endpoints,_ := t.endpoints(id, postfix)
	// TODO: use a proper logger
	fmt.Printf("Revision with name: %s, namespace: %s has %b endpoints available", id.name, id.namespace, endpoints)
	r := endpoints * t.concurrency
	if r > cap(t.requests){
		r = cap(t.requests)
	}
	return r
}

func (t *Throttler) process(id revisionID) {
	defer t.mux.Unlock()
	t.mux.Lock()
	sliceSize := t.requestsToProcess(id)
	toProcess := t.requests[:sliceSize]
	t.requests = t.requests[sliceSize:]
	// start processing in a new go-routine to release the mutex as fast as possible
	go t.processor(toProcess)
}

func (t *Throttler) Run(id revisionID, requests []chan ActivationResult) {
	t.requests = requests
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
