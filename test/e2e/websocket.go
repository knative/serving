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
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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
			t.Logf("Connection failed: %v", err)
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
	for i := 0; i < size; i++ {
		now := time.Now()
		for {
			if time.Since(now) > uniqueHostConnTimeout {
				return nil, fmt.Errorf("timed out finding the %dth connection", i)
			}
			if int(time.Since(now).Seconds()) % 2 == 0 {
				uniqueHostConns.Range(func(key, value interface{}) bool {
					if conn, ok := value.(*websocket.Conn); ok {
						if err := conn.WriteMessage(websocket.TextMessage, []byte("keepAlive")); err != nil {
							return false
						}
						_, recv, err := conn.ReadMessage()
						if err != nil {
							return false
						}
						host := string(recv)
						hour, min, sec := time.Now().Clock()
						fmt.Printf("\n\n in uniqueHostConnections, pinging: %s at time %v:%v:%v\n\n", host, hour, min, sec)
					}
					return true
				})
			}
			conn, err := connect(t, clients, names.URL.Hostname())
			if err != nil {
				continue
			}
			if err = conn.WriteMessage(websocket.TextMessage, []byte("hostname")); err != nil {
				continue
			}
			_, recv, err := conn.ReadMessage()
			if err != nil {
				continue
			}
			host := string(recv)
			if host == "" {
				continue
			}
			if _, ok := uniqueHostConns.LoadOrStore(host, conn); !ok {
				t.Logf("New pod has been discovered: %s", host)
				break
			} else {
				t.Logf("Existing pod has been returned: %s", host)
				conn.Close()
			}
		}
	}
	length := 0
	uniqueHostConns.Range(func(key, _ interface{}) bool {
		length++
		t.Logf("--------------- Printing unique host conns: key %v", key)
		return true
	})
	if length != size {
		t.Fatalf("Did not create %d unique pods, actually created %d", size, length)
	}
	return uniqueHostConns, nil
}

// deleteHostConnections closes and removes x number of open websocket connections
// from the hostConnMap where x == size
func deleteHostConnections(t *testing.T, hostConnMap *sync.Map, size int) error {
	hostConnMap.Range(func(key, value interface{}) bool {
		if size <= 0 {
			return false
		}

		if conn, ok := value.(*websocket.Conn); ok {
			conn.Close()
			hostConnMap.Delete(key)
			size -= 1
			t.Logf("Closed connection to pod: %s, size is now %d", key.(string), size)
			return true
		}
		return false
	})

	if size != 0 {
		return errors.New("failed to close connections")
	}

	return nil
}

// pingOpenConnections tries to keep the websocket connection alive by
// sending a keepAlive message every 3 seconds. Otherwise the connection drops
// after ~60 seconds and makes the tests flakey
func pingOpenConnections(doneCh chan struct{}, hostConnMap *sync.Map) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			hostConnMap.Range(func(key, value interface{}) bool {
				if conn, ok := value.(*websocket.Conn); ok {
					if err := conn.WriteMessage(websocket.TextMessage, []byte("keepAlive")); err != nil {
						return false
					}
					_, recv, err := conn.ReadMessage()
					if err != nil {
						return false
					}

					host := string(recv)
					hour, min, sec := time.Now().Clock()
					fmt.Printf("\n\n pingOpenConnections: %s at time %v:%v:%v\n\n", host, hour, min, sec)
					return true
				}
				return true
			})
		case <-doneCh:
			return nil
		}
	}
}
