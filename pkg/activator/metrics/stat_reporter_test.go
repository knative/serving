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

package activator

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	gorillawebsocket "github.com/gorilla/websocket"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/autoscaler/metrics"
	"knative.dev/serving/pkg/autoscaler/metrics/protocol"
)

func TestReportStats(t *testing.T) {
	logger := logtesting.TestLogger(t)
	ch := make(chan []metrics.StatMessage)

	results := make(chan []byte)
	sink := sendRawFunc(func(msgType int, msg []byte) error {
		if msgType != gorillawebsocket.BinaryMessage {
			t.Errorf("Expected metrics to be sent as Binary (%d), was %d", gorillawebsocket.BinaryMessage, msgType)
		}

		results <- msg
		return nil
	})

	defer close(ch)
	go ReportStats(logger, sink, ch)

	inputs := [][]metrics.StatMessage{{{
		Key: types.NamespacedName{Name: "first-a"},
	}, {
		Key: types.NamespacedName{Name: "first-b"},
	}}, {{
		Key: types.NamespacedName{Name: "second-a"},
	}, {
		Key: types.NamespacedName{Name: "second-b"},
	}}}

	for _, input := range inputs {
		ch <- input
	}

	received := make(chan struct{})
	output := make([][]byte, len(inputs))
	go func() {
		for i := 0; i < len(inputs); i++ {
			output[i] = <-results
		}
		close(received)
	}()

	select {
	case <-received:
		var statNames []string
		for _, b := range output {
			var wsms protocol.WireStatMessages
			if err := wsms.Unmarshal(b); err != nil {
				t.Errorf("Unmarshal stats = %v, expected no error", err)
			}

			for _, m := range wsms.Messages {
				statNames = append(statNames, metrics.ToStatMessage(*m).Key.Name)
			}
		}
		want := sets.NewString("first-a", "first-b", "second-a", "second-b")
		if got := sets.NewString(statNames...); !got.Equal(want) {
			t.Error("Expected to receive all stats (-want, +got):", cmp.Diff(want.List(), got.List()))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Did not receive results after 2 seconds")
	}
}

type sendRawFunc func(msgType int, msg []byte) error

func (fn sendRawFunc) SendRaw(msgType int, msg []byte) error {
	return fn(msgType, msg)
}
