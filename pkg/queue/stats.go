/*
Copyright 2018 Google Inc. All Rights Reserved.
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
	"time"

	"github.com/knative/serving/pkg/autoscaler"
)

// Poke is a token to push onto Stats Channels for recording requests stats.
type Poke struct{}

// Channels is a structure for holding the channels for driving Stats.
// It's just to make the NewStats signature easier to read.
type Channels struct {
	// Ticks with every request arrived
	ReqInChan chan Poke
	// Ticks with every request completed
	ReqOutChan chan Poke
	// Ticks at every quantization interval
	QuantizationChan <-chan time.Time
	// Ticks with every stat report request
	ReportChan <-chan time.Time
	// Stat reporting channel
	StatChan chan *autoscaler.Stat
}

// Stats is a structure for holding channels per pod.
type Stats struct {
	podName string
	ch      Channels
}

// NewStats instantiates a new instance of Stats.
func NewStats(podName string, channels Channels) *Stats {
	s := &Stats{
		podName: podName,
		ch:      channels,
	}

	go func() {
		var requestCount int32
		var concurrentCount int32
		var bucketedRequestCount int32
		buckets := make([]int32, 0)
		for {
			select {
			case <-s.ch.ReqInChan:
				requestCount = requestCount + 1
				concurrentCount = concurrentCount + 1
			case <-s.ch.QuantizationChan:
				// Calculate average concurrency for the current
				// quantum of time (bucket).
				buckets = append(buckets, concurrentCount)
				// Count the number of requests during bucketed
				// period
				bucketedRequestCount = bucketedRequestCount + requestCount
				requestCount = 0
				// Drain the request out channel only after the
				// bucket statistic has been recorded.  This
				// ensures that very fast requests are not missed
				// by making the smallest granularity of
				// concurrency one quantum of time.
			DrainReqOutChan:
				for {
					select {
					case <-s.ch.ReqOutChan:
						concurrentCount = concurrentCount - 1
					default:
						break DrainReqOutChan
					}
				}
			case now := <-s.ch.ReportChan:
				// Report the average bucket level. Does not
				// include the current bucket.
				var total float64
				var count float64
				for _, val := range buckets {
					total = total + float64(val)
					count = count + 1
				}
				var avg float64
				if count != 0 {
					avg = total / count
				}
				stat := &autoscaler.Stat{
					Time:                      &now,
					PodName:                   s.podName,
					AverageConcurrentRequests: avg,
					RequestCount:              bucketedRequestCount,
				}
				// Send the stat to another goroutine to transmit
				// so we can continue bucketing stats.
				s.ch.StatChan <- stat
				// Reset the stat counts which have been reported.
				bucketedRequestCount = 0
				buckets = make([]int32, 0)
			}
		}
	}()

	return s
}
