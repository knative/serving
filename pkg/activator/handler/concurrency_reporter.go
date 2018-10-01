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

package handler

import (
	"time"

	"github.com/knative/serving/pkg/autoscaler"
)

type ReqEvent struct {
	Key       string
	EventType ReqEventType
}

// Tokens to record ReqIn (request in) and ReqOut (request out) events respectively
type ReqEventType int

const (
	ReqIn ReqEventType = iota
	ReqOut
)

// Channels is a structure for holding the channels for driving Stats.
// It's just to make the NewStats signature easier to read.
type Channels struct {
	// Ticks with every request arrived/completed respectively
	ReqChan chan ReqEvent
	// Ticks with every stat report request
	ReportChan <-chan time.Time
	// Stat reporting channel
	StatChan chan *autoscaler.StatMessage
}

// NewStats instantiates a new instance of Stats.
func NewConcurrencyReporter(podName string, channels Channels) {

	go func() {
		concurrencyPerKey := make(map[string]int32)
		for {
			select {
			case event := <-channels.ReqChan:
				switch event.EventType {
				case ReqIn:
					concurrencyPerKey[event.Key]++
				case ReqOut:
					concurrencyPerKey[event.Key]--

				}
			case now := <-channels.ReportChan:
				for key, concurrency := range concurrencyPerKey {
					if concurrency == 0 {
						delete(concurrencyPerKey, key)
					} else {
						stat := autoscaler.Stat{
							Time:                      &now,
							PodName:                   podName,
							AverageConcurrentRequests: float64(concurrency),
							RequestCount:              0,
						}

						// Send the stat to another goroutine to transmit
						// so we can continue bucketing stats.
						channels.StatChan <- &autoscaler.StatMessage{
							Key:  key,
							Stat: stat,
						}
					}
				}
			}
		}
	}()
}
