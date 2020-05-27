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

package e2e

import (
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/sync/errgroup"

	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/util/wait"
	pkgTest "knative.dev/pkg/test"
	ingress "knative.dev/pkg/test/ingress"
	"knative.dev/serving/test"
)

const (
	connectRetryInterval  = 1 * time.Second
	connectTimeout        = 1 * time.Minute
	uniqueHostConnTimeout = 3 * time.Minute
)

// connect attempts to establish WebSocket connection with the Service.
// It will retry until reaching `connectTimeout` duration.
func connect(t *testing.T, clients *test.Clients, domain string) (*websocket.Conn, error) {
	var (
		err     error
		address string
	)

	if test.ServingFlags.ResolvableDomain {
		address = domain
	} else if pkgTest.Flags.IngressEndpoint != "" {
		address = pkgTest.Flags.IngressEndpoint
	} else if address, err = ingress.GetIngressEndpoint(clients.KubeClient.Kube); err != nil {
		return nil, err
	}

	u := url.URL{Scheme: "ws", Host: address, Path: "/"}
	var conn *websocket.Conn
	waitErr := wait.PollImmediate(connectRetryInterval, connectTimeout, func() (bool, error) {
		t.Logf("Connecting using websocket: url=%s, host=%s", u.String(), domain)
		c, resp, err := websocket.DefaultDialer.Dial(u.String(), http.Header{"Host": {domain}})
		if err == nil {
			t.Log("WebSocket connection established.")
			conn = c
			return true, nil
		}
		if resp == nil {
			// We don't have an HTTP response, probably TCP errors.
			t.Log("Connection failed:", err)
			return false, nil
		}

		body, readErr := ioutil.ReadAll(resp.Body)
		if readErr != nil {
			t.Logf("Connection failed: %v. Failed to read HTTP response: %v", err, readErr)
			return false, nil
		}
		t.Logf("HTTP connection failed: %v. Response=%+v. ResponseBody=%q", err, resp, body)
		return false, nil
	})
	return conn, waitErr
}

// uniqueHostConnections returns a map of open websocket connections to x number of pods where
// x == size; ensuring that each connection lands on a separate pod
func uniqueHostConnections(t *testing.T, names test.ResourceNames, size int) (*sync.Map, error) {
	clients := Setup(t)
	uniqueHostConns := &sync.Map{}

	reqCnt := int32(0)
	ctx, _ := context.WithTimeout(context.Background(), uniqueHostConnTimeout)
	gr, gctx := errgroup.WithContext(ctx)
	for i := 0; i < size; i++ {
		gr.Go(func() error {
			for {
				select {
				case <-gctx.Done():
					return errors.New("timed out trying to find unique host connections")

				default:
					atomic.AddInt32(&reqCnt, 1)
					conn, err := connect(t, clients, names.URL.Hostname())
					if err != nil {
						return err
					}

					if err = conn.WriteMessage(websocket.TextMessage, []byte("hostname")); err != nil {
						return err
					}

					_, recv, err := conn.ReadMessage()
					if err != nil {
						return err
					}

					host := string(recv)
					if host == "" {
						return errors.New("no host name is received from the server")
					}

					if _, ok := uniqueHostConns.LoadOrStore(host, conn); !ok {
						t.Log("New pod has been discovered:", host)
						return nil
					}
					t.Log("Existing pod has been returned:", host)
					conn.Close()
				}
			}
		})
	}

	if err := gr.Wait(); err != nil {
		return nil, err
	}
	t.Logf("For %d pods a total of %d requests were made", size, reqCnt)
	return uniqueHostConns, nil
}

// deleteHostConnections closes and removees x number of open websocket connections
// from the hostConnMap where x == size
func deleteHostConnections(hostConnMap *sync.Map, size int) error {
	hostConnMap.Range(func(key, value interface{}) bool {
		if size <= 0 {
			return false
		}

		if conn, ok := value.(*websocket.Conn); ok {
			conn.Close()
			hostConnMap.Delete(key)
			size -= 1
			return true
		}
		return false
	})

	if size != 0 {
		return errors.New("Failed to close connections")
	}

	return nil
}
