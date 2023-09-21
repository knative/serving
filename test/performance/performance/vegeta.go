/*
Copyright 2023 The Knative Authors

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

package performance

import (
	"log"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

// VegetaReporter wraps vegeta.Metrics to report metrics via channels.
// Call StopAndCollectMetrics() to finish reporting and return the collected metrics.
type VegetaReporter struct {
	metrics *vegeta.Metrics
	results chan *vegeta.Result
	stop    chan bool
}

// NewVegetaReporter creates a new VegetaReporter.
func NewVegetaReporter() *VegetaReporter {
	reporter := &VegetaReporter{
		metrics: &vegeta.Metrics{},
		results: make(chan *vegeta.Result, 1000),
		stop:    make(chan bool),
	}

	go reporter.run()

	return reporter
}

// AddResult adds a results asynchronously.
func (vr *VegetaReporter) AddResult(r *vegeta.Result) {
	vr.results <- r
}

// StopAndCollectMetrics flushes the metrics and collects latency distribution,
// shuts down the reporter and returns the collected metrics.
func (vr *VegetaReporter) StopAndCollectMetrics() *vegeta.Metrics {
	log.Println("Shutting down VegetaReporter")
	vr.stop <- true
	vr.metrics.Close()
	return vr.metrics
}

func (vr *VegetaReporter) run() {
	for {
		select {
		case r := <-vr.results:
			vr.metrics.Add(r)
		case <-vr.stop:
			return
		}
	}
}
