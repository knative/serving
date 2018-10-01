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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/autoscaler"
)

func TestMultipleDifferentKeys(t *testing.T) {

	pod1 := "pod1"
	pod2 := "pod2"

	s := Channels{
		ReqChan:    make(chan ReqEvent),
		ReportChan: make(chan time.Time),
		StatChan:   make(chan *autoscaler.StatMessage),
	}
	NewConcurrencyReporter(autoscaler.ActivatorPodName, s)

	s.requestStart(pod1)
	s.requestStart(pod1)
	s.requestStart(pod2)

	now := time.Now()
	expectStats(t, s.report(now, 2), []*autoscaler.StatMessage{
		&autoscaler.StatMessage{
			Key: pod1,
			Stat: autoscaler.Stat{
				Time:                      &now,
				PodName:                   autoscaler.ActivatorPodName,
				AverageConcurrentRequests: 2.0,
				RequestCount:              0,
			},
		},
		&autoscaler.StatMessage{
			Key: pod2,
			Stat: autoscaler.Stat{
				Time:                      &now,
				PodName:                   autoscaler.ActivatorPodName,
				AverageConcurrentRequests: 1.0,
				RequestCount:              0,
			},
		},
	})

	s.requestEnd(pod2)
	s.requestEnd(pod1)

	now = time.Now()
	expectStats(t, s.report(now, 1), []*autoscaler.StatMessage{
		&autoscaler.StatMessage{
			Key: pod1,
			Stat: autoscaler.Stat{
				Time:                      &now,
				PodName:                   autoscaler.ActivatorPodName,
				AverageConcurrentRequests: 1.0,
				RequestCount:              0,
			},
		},
	})
}

func (s *Channels) requestStart(key string) {
	s.ReqChan <- ReqEvent{Key: key, EventType: ReqIn}
}

func (s *Channels) requestEnd(key string) {
	s.ReqChan <- ReqEvent{Key: key, EventType: ReqOut}
}

func (s *Channels) report(t time.Time, count int) []*autoscaler.StatMessage {
	s.ReportChan <- t
	metrics := make([]*autoscaler.StatMessage, count)
	for i := 0; i < count; i++ {
		metrics[i] = <-s.StatChan
	}
	return metrics
}

func expectStats(t *testing.T, gots, wants []*autoscaler.StatMessage) {
	// Sort the stats to guarantee a given order
	sorter := cmpopts.SortSlices(func(a, b *autoscaler.StatMessage) bool {
		return a.Key < b.Key
	})
	if diff := cmp.Diff(wants, gots, sorter); diff != "" {
		t.Errorf("Unexpected stats (-want +got): %v", diff)
	}
}
