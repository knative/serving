/*
Copyright 2020 The Knative Authors

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

package metrics

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"k8s.io/apimachinery/pkg/types"
)

func TestStatMessageConversion(t *testing.T) {
	sm := StatMessage{
		Key: types.NamespacedName{
			Namespace: "test-namespace",
			Name:      "test-name",
		},
		Stat: Stat{
			PodName:                          "test",
			AverageConcurrentRequests:        1.1,
			AverageProxiedConcurrentRequests: 2.1,
			RequestCount:                     50,
			ProxiedRequestCount:              100,
		},
	}

	wsm := &WireStatMessage{
		Namespace: sm.Key.Namespace,
		Name:      sm.Key.Name,
		Stat:      &sm.Stat,
	}

	if got, want := sm.ToWireStatMessage(), wsm; !cmp.Equal(got, want) {
		t.Fatal("WireStatMessage mismatch: diff (-got, +want)", cmp.Diff(got, want))
	}

	if got, want := wsm.ToStatMessage(), sm; !cmp.Equal(got, want) {
		t.Fatal("StatMessage mismatch: diff (-got, +want)", cmp.Diff(got, want))
	}
}

func TestStatMessageSliceConversion(t *testing.T) {
	sm1 := StatMessage{
		Key: types.NamespacedName{
			Namespace: "test-namespace",
			Name:      "test-name",
		},
		Stat: Stat{
			PodName:                          "test",
			AverageConcurrentRequests:        1.1,
			AverageProxiedConcurrentRequests: 2.1,
			RequestCount:                     50,
			ProxiedRequestCount:              100,
		},
	}
	sm2 := StatMessage{
		Key: types.NamespacedName{
			Namespace: "test-namespace2",
			Name:      "test-name2",
		},
		Stat: Stat{
			PodName:                          "test2",
			AverageConcurrentRequests:        2.1,
			AverageProxiedConcurrentRequests: 3.1,
			RequestCount:                     75,
			ProxiedRequestCount:              125,
		},
	}

	sms := []StatMessage{sm1, sm2}

	wsm1 := sm1.ToWireStatMessage()
	wsm2 := sm2.ToWireStatMessage()
	wsms := WireStatMessages{Messages: []*WireStatMessage{wsm1, wsm2}}

	if got, want := ToWireStatMessages(sms), wsms; !cmp.Equal(got, want) {
		t.Fatal("WireStatMessages mismatch: diff (-got, +want)", cmp.Diff(got, want))
	}
}
