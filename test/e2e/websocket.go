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
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
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
func uniqueHostConnections(t *testing.T, names test.ResourceNames, size int) (map[string]*websocket.Conn, error) {
	clients := Setup(t)
	uniqueHostConns := map[string]*websocket.Conn{}
	for i := 0; i < size; i++ {
		now := time.Now()
		for {
			if time.Since(now) > uniqueHostConnTimeout {
				return nil, fmt.Errorf("timed out finding the %dth connection", i)
			}
			if int(time.Since(now).Seconds()) % 2 == 0 {
				for key, conn := range uniqueHostConns {
					hour, min, sec := time.Now().Clock()
					if err := conn.WriteMessage(websocket.TextMessage, []byte("keepAlive")); err != nil {
						return nil, fmt.Errorf("write message failed for host %v at %v:%v:%v", key, hour, min, sec)
					}
					_, _, err := conn.ReadMessage()// _, recv, err := conn.ReadMessage()
					if err != nil {
						return nil, fmt.Errorf("read message failed for host %v at %v:%v:%v", key, hour, min, sec)
					}
					//host := string(recv)
					//hour, min, sec := time.Now().Clock()
					//fmt.Printf("\n\n in uniqueHostConnections, pinging: %s at time %v:%v:%v\n\n", host, hour, min, sec)
				}
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
			if _, ok := uniqueHostConns[host]; !ok {
				uniqueHostConns[host] = conn
				t.Logf("New pod has been discovered: %s", host)
				break
			} else {
				t.Logf("Existing pod has been returned: %s", host)
				conn.Close()
			}
		}
	}
	if len(uniqueHostConns) != size {
		t.Fatalf("Did not create %d unique pods, actually created %d", size, len(uniqueHostConns))
	}
	for key, _ := range uniqueHostConns {
		fmt.Printf("\n\n\n created uniqueHostConns %#v\n\n\n", key)
	}
	return uniqueHostConns, nil
}

// deleteHostConnections closes and removes x number of open websocket connections
// from the hostConnMap where x == size
func deleteHostConnections(t *testing.T, hostConnMap map[string]*websocket.Conn, size int) error {
	for key, conn := range hostConnMap {
		if size == 0 {
			return nil
		}
		conn.Close()
		delete(hostConnMap, key)
		size -= 1
		t.Logf("Closed connection to pod: %s, size is now %d", key, size)
	}
	return nil
}

// pingOpenConnections tries to keep the websocket connection alive by
// sending a keepAlive message every 3 seconds. Otherwise the connection drops
// after ~60 seconds and makes the tests flakey
func pingOpenConnections(hostConnMap map[string]*websocket.Conn) error {
	for _, conn := range hostConnMap {
		if err := conn.WriteMessage(websocket.TextMessage, []byte("keepAlive")); err != nil {
			return err
		}
		_, recv, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		host := string(recv)
		hour, min, sec := time.Now().Clock()
		fmt.Printf("\n\n in pingOpenConnections, pinging: %s at time %v:%v:%v\n\n", host, hour, min, sec)
	}
	return nil
}
