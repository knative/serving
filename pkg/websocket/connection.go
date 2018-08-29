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
	"io"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/gorilla/websocket"
)

var (
	// ErrConnectionNotEstablished is returned by methods that need a connection
	// but no connection is already created.
	ErrConnectionNotEstablished = errors.New("connection has not yet been established")
)

// RawConnection is an interface defining the methods needed
// from a websocket connection
type RawConnection interface {
	WriteMessage(messageType int, data []byte) error
	NextReader() (int, io.Reader, error)
	Close() error
}

// ManagedConnection represents a websocket connection.
type ManagedConnection struct {
	connFactory    func() (RawConnection, error)
	connection     RawConnection
	messageBuffer  *bytes.Buffer
	messageEncoder *gob.Encoder
	IsClosed       bool
}

// NewDurableSendingConnection creates a new websocket connection
// that can only send messages to the endpoint it connects to.
// The connection will continuously be kept alive and reconnected
// in case of a loss of connectivity.
func NewDurableSendingConnection(connFactory func() (RawConnection, error)) *ManagedConnection {
	conn := newConnection(connFactory)

	// Keep the connection alive asynchronously and reconnect on
	// connection failure.
	go func() {
		for {
			if conn.IsClosed {
				return
			}
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

// newConnection creates a new connection primitive.
func newConnection(connFactory func() (RawConnection, error)) *ManagedConnection {
	buffer := &bytes.Buffer{}
	conn := &ManagedConnection{
		connFactory:    connFactory,
		connection:     nil,
		messageBuffer:  buffer,
		messageEncoder: gob.NewEncoder(buffer),
		IsClosed:       false,
	}

	return conn
}

// connect tries to establish a websocket connection.
func (c *ManagedConnection) connect() (err error) {
	wait.ExponentialBackoff(wait.Backoff{
		Duration: 100 * time.Millisecond,
		Factor:   1.3,
		Steps:    20,
		Jitter:   0.5,
	}, func() (bool, error) {
		c.connection, err = c.connFactory()
		if err != nil {
			return false, nil
		}
		return true, nil
	})

	return err
}

// Send sends an encodable message over the websocket connection.
func (c *ManagedConnection) Send(msg interface{}) error {
	if c.connection == nil {
		return ErrConnectionNotEstablished
	}

	if err := c.messageEncoder.Encode(msg); err != nil {
		return err
	}

	return c.connection.WriteMessage(websocket.BinaryMessage, c.messageBuffer.Bytes())
}

// Close closes the websocket connection.
func (c *ManagedConnection) Close() error {
	c.IsClosed = true
	if c.connection != nil {
		return c.connection.Close()
	}
	return nil
}
