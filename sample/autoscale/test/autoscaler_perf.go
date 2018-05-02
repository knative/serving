package main

import (
	"fmt"
	"net/http"
	"net/http/httptrace"
	"os"
	"time"
)

func sendRequest(mainStart time.Time) {
	req, _ := http.NewRequest("GET", "http://35.202.165.90/primes/40000000", nil)
	req.Host = "autoscale-route.default.demo-domain.com"
	var start time.Time
	var end time.Time

	trace := &httptrace.ClientTrace{
		ConnectDone: func(network, addr string, err error) {
			start = time.Now()
		},
		WroteRequest: func(wr httptrace.WroteRequestInfo) {
			end = time.Now()
		},
	}

	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		os.Stderr.WriteString(err.Error())
	}
	defer resp.Body.Close()
	fmt.Printf("%f,%f,%d\n", time.Since(mainStart).Seconds(), end.Sub(start).Seconds(), resp.StatusCode)
}

func sendRequestsQPS(mainStart time.Time, n int) {
	for i := 0; i < n; i++ {
		go sendRequest(mainStart)
	}
}

// This program sends 1 request to activate the revision, and then sends requests at
// 100 QPS every 2 second for 120 seconds. The standard output has 3 columns, they are:
// the time from the experiment starts, the http response time, and the http response code.
// Before run this program, update the service IP in sendRequest function, which can be queried
// with instructions here https://github.com/elafros/elafros/tree/master/sample/autoscale.
// Then run the program: go run sample/autoscale/test/autoscaler_perf.go
func main() {
	mainStart := time.Now()
	sendRequest(mainStart)
	time.Sleep(2 * time.Second)

	for i := 1; i <= 60; i++ {
		sendRequestsQPS(mainStart, 100)
		time.Sleep(2 * time.Second)
	}

	time.Sleep(time.Second * 300)
}
