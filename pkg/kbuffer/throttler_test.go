package kbuffer

import (
	"testing"
	"time"
)

var tickerSleeps = time.Millisecond

var reqPerContainer = 1

var totalRequests = 2

var rev = revisionID{"default", "hello"}

func getMockedProcessor(actuallyProcessed *int) func([]interface{}) {
	return func(reqs []interface{}) {
		*actuallyProcessed+=len(reqs)
	}
}

func endpointsGetter(endpoints int) func(id revisionID) int {
return func(id revisionID) int {
	return endpoints
}
}

func startThrottler(actuallyProcesssed *int, totalRequests int, reqPerContainer int, sleep time.Duration, endpoints int, ticks ...int) *Throttler {
	requests := make([]interface{}, totalRequests)
	mockedProcessor := getMockedProcessor(actuallyProcesssed)
	ticker := NewTicker(time.Millisecond)
	throttler := NewThrottler(requests, reqPerContainer, endpointsGetter(endpoints), mockedProcessor, ticker)
	go throttler.Run(rev)
	go ticker.Tick(ticks...)
	return throttler
}

func assertEqualRequests(want, got int, t *testing.T) {
	if want != got {
		t.Errorf("The number of requests - %d was not equal to %d after the update", got, want)
	}
}

// Assure that each tick processed the number of allowed requests
func TestThrottler_Process_Part_Of_The_Requests(t *testing.T) {
	// use a local var to avoid collisions across tests
	var actuallyProcessed int
	endpoints := 1
	shouldBeProcessed := 1
	shouldBeLeftInTheQueue := 1

	throttler := startThrottler(&actuallyProcessed, totalRequests, reqPerContainer, tickerSleeps, endpoints,1)

	time.Sleep(tickerSleeps)
	assertEqualRequests(shouldBeLeftInTheQueue, cap(throttler.requests), t)
	assertEqualRequests(shouldBeProcessed, actuallyProcessed, t)
}

// Assure all requests are processed
func TestThrottler_Process_All_Requests(t *testing.T) {
	var actuallyProcessed int
	endpoints := 1
	shouldBeProcessed := 2
	shouldBeLeftInTheQueue := 0

	throttler := startThrottler(&actuallyProcessed, totalRequests, reqPerContainer, tickerSleeps, endpoints,2)

	// sleep at least as much as the ticker does
	time.Sleep(tickerSleeps * time.Duration(totalRequests))
	assertEqualRequests(shouldBeLeftInTheQueue, cap(throttler.requests), t)
	assertEqualRequests(shouldBeProcessed, actuallyProcessed, t)
}

// Assure the ticker is stopped after all requests are processed
func TestThrottler_Stop_Ticker(t *testing.T) {
	var actuallyProcessed int
	endpoints := 1
	shouldBeProcessed := 2
	shouldBeLeftInTheQueue := 0

	// The queue will be emptied before the ticks are exhausted, the ticks are infinite in this case
	throttler := startThrottler(&actuallyProcessed, totalRequests, reqPerContainer, tickerSleeps, endpoints)

	time.Sleep(10 * time.Millisecond)

	if !throttler.ticker.stopped {
		t.Errorf("The ticker is not stopped")
	}
	if !throttler.stopped {
		t.Errorf("The throttler is not stopped")
	}
	assertEqualRequests(shouldBeLeftInTheQueue, cap(throttler.requests), t)
	assertEqualRequests(shouldBeProcessed, actuallyProcessed, t)
}

// Assure the number of parallel executions proportional to the number of endpoints
func TestThrottler_Several_Endpoints(t *testing.T) {
	var actuallyProcessed int
	endpoints := 2
	shouldBeProcessed := 2
	shouldBeLeftInTheQueue := 0

	throttler := startThrottler(&actuallyProcessed, totalRequests, reqPerContainer, tickerSleeps, endpoints,1)

	time.Sleep(tickerSleeps)

	assertEqualRequests(shouldBeLeftInTheQueue, cap(throttler.requests), t)
	assertEqualRequests(shouldBeProcessed, actuallyProcessed, t)
}

// Assure we don't fail if the number of endpoints is bigger then the number of requests
func TestThrottler_More_Endpoints_Then_Requests(t *testing.T) {
	var actuallyProcessed int
	endpoints := 3
	shouldBeProcessed := 2
	shouldBeLeftInTheQueue := 0

	throttler := startThrottler(&actuallyProcessed, totalRequests, reqPerContainer, tickerSleeps, endpoints,1)

	time.Sleep(tickerSleeps)

	assertEqualRequests(shouldBeLeftInTheQueue, cap(throttler.requests), t)
	assertEqualRequests(shouldBeProcessed, actuallyProcessed, t)
}