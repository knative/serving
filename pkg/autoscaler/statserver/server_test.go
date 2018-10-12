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

package statserver_test

import (
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/knative/serving/pkg/autoscaler"
	stats "github.com/knative/serving/pkg/autoscaler/statserver"
)
 
func TestServerLifecycle(t *testing.T) {
	statsCh := make(chan *autoscaler.StatMessage)
	server := stats.NewTestServer(statsCh)

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := server.ListenAndServe()
		if err != nil {
			t.Fatal("ListenAndServe failed.", err)
		}
	}()

	server.ListenAddr()
	server.Shutdown(time.Second)

	wg.Wait()
}

func newStatMessage(revKey string, podName string, averageConcurrentRequests float64, requestCount int32) *autoscaler.StatMessage {
	now := time.Now()
	return &autoscaler.StatMessage{
		revKey,
		autoscaler.Stat{
			Time:                      &now,
			PodName:                   podName,
			AverageConcurrentRequests: averageConcurrentRequests,
			RequestCount:              requestCount,
		},
	}
}

func dial(serverURL string, t *testing.T) (*websocket.Conn, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Scheme = "ws"

	dialer := &websocket.Dialer{
		HandshakeTimeout: time.Second,
	}
	statSink, _, err := dialer.Dial(u.String(), nil)
	return statSink, err
}
