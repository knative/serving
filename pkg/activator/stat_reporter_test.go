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
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	gorillawebsocket "github.com/gorilla/websocket"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/serving/pkg/autoscaler/metrics"
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
		for i := range inputs {
			output[i] = <-results
		}
		close(received)
	}()

	select {
	case <-received:
		var statNames []string
		for _, b := range output {
			var wsms metrics.WireStatMessages
			if err := wsms.Unmarshal(b); err != nil {
				t.Errorf("Unmarshal stats = %v, expected no error", err)
			}

			for _, m := range wsms.Messages {
				statNames = append(statNames, m.ToStatMessage().Key.Name)
			}
		}
		want := sets.New("first-a", "first-b", "second-a", "second-b")
		if got := sets.New(statNames...); !got.Equal(want) {
			t.Error("Expected to receive all stats (-want, +got):", cmp.Diff(sets.List(want), sets.List(got)))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Did not receive results after 2 seconds")
	}
}

type sendRawFunc func(msgType int, msg []byte) error

func (fn sendRawFunc) SendRaw(msgType int, msg []byte) error {
	return fn(msgType, msg)
}

func TestReportStatsSendFailure(t *testing.T) {
	logger := logtesting.TestLogger(t)
	ch := make(chan []metrics.StatMessage)

	sendErr := errors.New("connection refused")
	errorReceived := make(chan struct{})
	sink := sendRawFunc(func(msgType int, msg []byte) error {
		close(errorReceived)
		return sendErr
	})

	defer close(ch)
	go ReportStats(logger, sink, ch)

	// Send a stat message
	ch <- []metrics.StatMessage{{
		Key: types.NamespacedName{Name: "test-revision"},
	}}

	// Wait for the error to be processed
	select {
	case <-errorReceived:
		// Success - the error path was executed
	case <-time.After(2 * time.Second):
		t.Fatal("SendRaw was not called within timeout")
	}

	// The TestLogger will panic if logs occur after the test runs.
	// This occurs in this test because ReportStats kicks off
	// goroutines with no lifecycle management. Meaning we cannot
	// wait for all routines to clean up prior to exiting the test.
	//
	// For now we include a sleep here to prevent said panic
	time.Sleep(10 * time.Millisecond)
}

func TestAutoscalerConnectionOptions(t *testing.T) {
	logger := logtesting.TestLogger(t)

	opts := AutoscalerConnectionOptions(logger, nil)

	if len(opts) != 2 {
		t.Errorf("Expected 2 connection options, got %d", len(opts))
	}
}
