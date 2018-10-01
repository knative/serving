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
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/autoscaler"
)

func TestNoData(t *testing.T) {

	pod1 := "pod1"
	pod2 := "pod2"

	s := Channels{}
	NewConcurrencyReporter(autoscaler.ActivatorPodName, s)

	fmt.Println("got here1")

	s.requestStart(pod1)
	fmt.Println("got here2")
	s.requestStart(pod1)
	s.requestStart(pod2)

	fmt.Println("got here")

	now := time.Now()
	got := s.report(now)

	want := &autoscaler.StatMessage{
		Key: "pod1",
		Stat: autoscaler.Stat{
			Time:                      &now,
			PodName:                   autoscaler.ActivatorPodName,
			AverageConcurrentRequests: 2.0,
			RequestCount:              0,
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Unexpected stat (-want +got): %v", diff)
	}
}

func (s *Channels) requestStart(key string) {
	s.ReqChan <- ReqEvent{Key: key, EventType: ReqIn}
}

func (s *Channels) requestEnd(key string) {
	s.ReqChan <- ReqEvent{Key: key, EventType: ReqOut}
}

func (s *Channels) report(t time.Time) *autoscaler.StatMessage {
	s.ReportChan <- t
	return <-s.StatChan
}
