package main

import (
	"fmt"
	"net/http"
	"net/http/httptrace"
	"os"
	"time"
)

var (
	qps    int
	url    string
	gStart time.Time
)

func sendRequest() {
	req, _ := http.NewRequest("GET", url, nil)
	req.Host = "route-example.default.demo-domain.com"
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
	fmt.Printf("%f,%f,%d\n", time.Since(gStart).Seconds(), end.Sub(start).Seconds(), resp.StatusCode)
}

// Distribute the load of qps evenly within one second.
func sendRequestsQPS() {
	doEvery(time.Duration(1000/qps)*time.Millisecond, time.Second, sendRequest)
}

// doEvery calls f every d for a time window of w.
func doEvery(d time.Duration, w time.Duration, f func()) {
	start := time.Now()
	for _ = range time.Tick(d) {
		go f()
		if time.Now().Sub(start) >= w {
			break
		}
	}
}

// This program sends 1 request to activate the revision, and then sends requests at
// 100 QPS every second for 120 seconds. The standard output has 3 columns, they are:
// the time from the experiment starts, the http response time, and the http response code.
// Before running this program, set the environment variable SERVICE_IP with
// instructions here https://github.com/elafros/elafros/tree/master/sample/helloworld.
// Then run the program: go run pkg/activator/test/perftest.go
func main() {
	url = fmt.Sprintf("http://%s/", os.Getenv("SERVICE_IP"))
	qps = 100
	gStart = time.Now()
	sendRequest()
	time.Sleep(2 * time.Second)
	doEvery(time.Second, 60*time.Second, sendRequestsQPS)
	time.Sleep(30 * time.Second)
}
