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
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/gorilla/websocket"
)

var (
	// ErrConnectionNotEstablished is returned by methods that need a connection
	// but no connection is already created.
	ErrConnectionNotEstablished = errors.New("connection has not yet been established")

	connFactory = func(target string) (rawConnection, error) {
		dialer := &websocket.Dialer{
			HandshakeTimeout: 3 * time.Second,
		}
		conn, _, err := dialer.Dial(target, nil)
		return conn, err
	}
)

// RawConnection is an interface defining the methods needed
// from a websocket connection
type rawConnection interface {
	WriteMessage(messageType int, data []byte) error
	NextReader() (int, io.Reader, error)
	Close() error
}

// ManagedConnection represents a websocket connection.
type ManagedConnection struct {
	target     string
	connection rawConnection
	closeChan  chan struct{}

	// This mutex controls access to the connection reference
	// itself.
	connectionLock sync.RWMutex

	// Gorilla's documentation states, that one reader and
	// one writer are allowed concurrently.
	readerLock sync.Mutex
	writerLock sync.Mutex
}

// NewDurableSendingConnection creates a new websocket connection
// that can only send messages to the endpoint it connects to.
// The connection will continuously be kept alive and reconnected
// in case of a loss of connectivity.
func NewDurableSendingConnection(target string) *ManagedConnection {
	c := newConnection(target)

	// Keep the connection alive asynchronously and reconnect on
	// connection failure.
	go func() {
		// If the close signal races the connection attempt, make
		// sure the connection actually closes.
		defer func() {
			c.connectionLock.RLock()
			defer c.connectionLock.RUnlock()

			if conn := c.connection; conn != nil {
				conn.Close()
			}
		}()
		for {
			select {
			default:
				if err := c.connect(); err != nil {
					continue
				}
				c.keepalive()
			case <-c.closeChan:
				return
			}
		}
	}()

	return c
}

// newConnection creates a new connection primitive.
func newConnection(target string) *ManagedConnection {
	conn := &ManagedConnection{
		target:    target,
		closeChan: make(chan struct{}, 1),
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
		conn, err := connFactory(c.target)
		if err != nil {
			return false, nil
		}
		c.connectionLock.Lock()
		defer c.connectionLock.Unlock()

		c.connection = conn
		return true, nil
	})

	return err
}

// keepalive keeps the connection open and reads control messages.
// All messages are discarded.
func (c *ManagedConnection) keepalive() (err error) {
	c.readerLock.Lock()
	defer c.readerLock.Unlock()

	for {
		func() {
			c.connectionLock.RLock()
			defer c.connectionLock.RUnlock()

			if conn := c.connection; conn != nil {
				if _, _, err = conn.NextReader(); err != nil {
					conn.Close()
				}
			}
		}()

		if err != nil {
			return err
		}
	}
}

// Send sends an encodable message over the websocket connection.
func (c *ManagedConnection) Send(msg interface{}) error {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()

	conn := c.connection
	if conn == nil {
		return ErrConnectionNotEstablished
	}

	c.writerLock.Lock()
	defer c.writerLock.Unlock()

	var b bytes.Buffer
	enc := gob.NewEncoder(&b)
	if err := enc.Encode(msg); err != nil {
		return err
	}

	return conn.WriteMessage(websocket.BinaryMessage, b.Bytes())
}

// Close closes the websocket connection.
func (c *ManagedConnection) Close() error {
	c.closeChan <- struct{}{}
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()

	if conn := c.connection; conn != nil {
		return conn.Close()
	}
	return nil
}
