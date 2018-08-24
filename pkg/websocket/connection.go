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

package websocket

import (
	"bytes"
	"encoding/gob"
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/gorilla/websocket"
)

// Connection represents a websocket connection.
type Connection struct {
	target         string
	connection     *websocket.Conn
	messageBuffer  *bytes.Buffer
	messageEncoder *gob.Encoder
}

// NewDurableSendingConnection creates a new websocket connection
// that can only send messages to the endpoint it connects to.
// The connection will continuously be kept alive and reconnected
// in case of a loss of connectivity.
func NewDurableSendingConnection(target string) *Connection {
	conn := newConnection(target)

	// Keep the connection alive asynchronously and reconnect on
	// connection failure.
	go func() {
		for {
			if err := conn.connect(); err != nil {
				continue
			}
			if _, _, err := conn.connection.NextReader(); err != nil {
				conn.connection.Close()
			}
		}
	}()

	return conn
}

// NewConnection creates a new websocket connection to the given target.
func newConnection(target string) *Connection {
	buffer := &bytes.Buffer{}
	return &Connection{
		target:         target,
		connection:     nil,
		messageBuffer:  buffer,
		messageEncoder: gob.NewEncoder(buffer),
	}
}

func (c *Connection) connect() (err error) {
	dialer := &websocket.Dialer{
		HandshakeTimeout: 3 * time.Second,
	}

	wait.ExponentialBackoff(wait.Backoff{
		Duration: 100 * time.Millisecond,
		Factor:   1.3,
		Steps:    20,
		Jitter:   0.5,
	}, func() (bool, error) {
		c.connection, _, err = dialer.Dial(c.target, nil)
		if err != nil {
			return false, nil
		}
		return true, nil
	})

	return err
}

// Send sends an encodable message over the websocket connection.
func (c *Connection) Send(msg interface{}) error {
	if c.connection == nil {
		return errors.New("connection has not yet been established")
	}

	if err := c.messageEncoder.Encode(msg); err != nil {
		return err
	}

	return c.connection.WriteMessage(websocket.BinaryMessage, c.messageBuffer.Bytes())
}

// Close closes the websocket connection.
func (c *Connection) Close() error {
	if c.connection != nil {
		return c.connection.Close()
	}
	return nil
}
