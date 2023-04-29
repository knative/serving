/*
Copyright 2019 The Knative Authors

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
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

var (
	// ErrConnectionNotEstablished is returned by methods that need a connection
	// but no connection is already created.
	ErrConnectionNotEstablished = errors.New("connection has not yet been established")

	// errShuttingDown is returned internally once the shutdown signal has been sent.
	errShuttingDown = errors.New("shutdown in progress")

	// pongTimeout defines the amount of time allowed between two pongs to arrive
	// before the connection is considered broken.
	pongTimeout = 10 * time.Second
)

// RawConnection is an interface defining the methods needed
// from a websocket connection
type rawConnection interface {
	Close() error
	SetReadDeadline(deadline time.Time) error
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
	WriteMessage(op ws.OpCode, p []byte) error
	NextReader() (ws.Header, io.Reader, error)
}

type NetConnExtension struct {
	conn net.Conn
}

func (nc *NetConnExtension) Read(p []byte) (n int, err error) {
	return nc.conn.Read(p)
}

func (nc *NetConnExtension) Write(p []byte) (n int, err error) {
	return nc.conn.Write(p)
}

func (nc *NetConnExtension) Close() error {
	return nc.conn.Close()
}

func (nc *NetConnExtension) SetReadDeadline(deadline time.Time) error {
	return nc.conn.SetReadDeadline(deadline)
}

func (nc *NetConnExtension) WriteMessage(op ws.OpCode, p []byte) error {
	return wsutil.WriteClientMessage(nc, op, p)
}

func (nc *NetConnExtension) NextReader() (ws.Header, io.Reader, error) {
	return wsutil.NextReader(nc, ws.StateClientSide)
}

// ManagedConnection represents a websocket connection.
type ManagedConnection struct {
	connection        rawConnection
	connectionFactory func() (rawConnection, error)

	closeChan chan struct{}
	closeOnce sync.Once

	establishChan chan struct{}
	establishOnce sync.Once

	// Used to capture asynchronous processes to be waited
	// on when shutting the connection down.
	processingWg sync.WaitGroup

	// If set, messages will be forwarded to this channel
	messageChan chan []byte

	// This mutex controls access to the connection reference
	// itself.
	connectionLock sync.RWMutex

	// Gorilla's documentation states, that one reader and
	// one writer are allowed concurrently.
	readerLock sync.Mutex
	writerLock sync.Mutex

	// Used for the exponential backoff when connecting
	connectionBackoff wait.Backoff
}

// NewDurableSendingConnection creates a new websocket connection
// that can only send messages to the endpoint it connects to.
// The connection will continuously be kept alive and reconnected
// in case of a loss of connectivity.
func NewDurableSendingConnection(target string, logger *zap.SugaredLogger) *ManagedConnection {
	return NewDurableConnection(target, nil, logger)
}

// NewDurableSendingConnectionGuaranteed creates a new websocket connection
// that can only send messages to the endpoint it connects to. It returns
// the connection if the connection can be established within the given
// `duration`. Otherwise it returns the ErrConnectionNotEstablished error.
//
// The connection will continuously be kept alive and reconnected
// in case of a loss of connectivity.
func NewDurableSendingConnectionGuaranteed(target string, duration time.Duration, logger *zap.SugaredLogger) (*ManagedConnection, error) {
	c := NewDurableConnection(target, nil, logger)

	select {
	case <-c.establishChan:
		return c, nil
	case <-time.After(duration):
		c.Shutdown()
		return nil, ErrConnectionNotEstablished
	}
}

// NewDurableConnection creates a new websocket connection, that
// passes incoming messages to the given message channel. It can also
// send messages to the endpoint it connects to.
// The connection will continuously be kept alive and reconnected
// in case of a loss of connectivity.
//
// Note: The given channel needs to be drained after calling `Shutdown`
// to not cause any deadlocks. If the channel's buffer is likely to be
// filled, this needs to happen in separate goroutines, i.e.
//
// go func() {conn.Shutdown(); close(messageChan)}
// go func() {for range messageChan {}}
func NewDurableConnection(target string, messageChan chan []byte, logger *zap.SugaredLogger) *ManagedConnection {
	ctx := context.TODO()
	websocketConnectionFactory := func() (rawConnection, error) {
		dialer := &ws.Dialer{
			// This needs to be relatively short to avoid the connection getting blackholed for a long time
			// by restarting the serving side of the connection behind a Kubernetes Service.
			Timeout: 3 * time.Second,
		}

		conn, _, _, err := dialer.Dial(ctx, target)
		if err != nil {
			if err != nil {
				logger.Errorw("Websocket connection could not be established", zap.Error(err))
			}
		}
		nc := &NetConnExtension{
			conn: conn,
		}
		return nc, err
	}

	c := newConnection(websocketConnectionFactory, messageChan)

	// Keep the connection alive asynchronously and reconnect on
	// connection failure.
	c.processingWg.Add(1)
	go func() {
		defer c.processingWg.Done()

		for {
			select {
			default:
				logger.Info("Connecting to ", target)
				if err := c.connect(); err != nil {
					logger.Errorw("Failed connecting to "+target, zap.Error(err))
					continue
				}
				logger.Debug("Connected to ", target)
				if err := c.keepalive(); err != nil {
					logger.Errorw(fmt.Sprintf("Connection to %s broke down, reconnecting...", target), zap.Error(err))
				}
				if err := c.closeConnection(); err != nil {
					logger.Errorw("Failed to close the connection after crashing", zap.Error(err))
				}
			case <-c.closeChan:
				logger.Infof("Connection to %s is being shutdown", target)
				return
			}
		}
	}()

	// Keep sending pings 3 times per pongTimeout interval.
	c.processingWg.Add(1)
	go func() {
		defer c.processingWg.Done()

		ticker := time.NewTicker(pongTimeout / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.write(ws.OpPing, []byte{}); err != nil {
					logger.Errorw("Failed to send ping message to "+target, zap.Error(err))
				}
			case <-c.closeChan:
				return
			}
		}
	}()

	return c
}

// newConnection creates a new connection primitive.
func newConnection(connFactory func() (rawConnection, error), messageChan chan []byte) *ManagedConnection {
	conn := &ManagedConnection{
		connectionFactory: connFactory,
		closeChan:         make(chan struct{}),
		establishChan:     make(chan struct{}),
		messageChan:       messageChan,
		connectionBackoff: wait.Backoff{
			Duration: 100 * time.Millisecond,
			Factor:   1.3,
			Steps:    20,
			Jitter:   0.5,
		},
	}

	return conn
}

// connect tries to establish a websocket connection.
func (c *ManagedConnection) connect() error {
	return wait.ExponentialBackoff(c.connectionBackoff, func() (bool, error) {
		select {
		default:
			conn, err := c.connectionFactory()
			if err != nil {
				return false, nil
			}

			// Setting the read deadline will cause NextReader in read
			// to fail if it is exceeded. This deadline is reset each
			// time we receive a pong message so we know the connection
			// is still intact.
			conn.SetReadDeadline(time.Now().Add(pongTimeout))

			c.connectionLock.Lock()
			defer c.connectionLock.Unlock()

			c.connection = conn
			c.establishOnce.Do(func() {
				close(c.establishChan)
			})
			return true, nil
		case <-c.closeChan:
			return false, errShuttingDown
		}
	})
}

// keepalive keeps the connection open.
func (c *ManagedConnection) keepalive() error {
	for {
		select {
		default:
			if err := c.read(); err != nil {
				return err
			}
		case <-c.closeChan:
			return errShuttingDown
		}
	}
}

// closeConnection closes the underlying websocket connection.
func (c *ManagedConnection) closeConnection() error {
	c.connectionLock.Lock()
	defer c.connectionLock.Unlock()

	if c.connection != nil {
		err := c.connection.Close()
		c.connection = nil
		return err
	}
	return nil
}

// read reads the next message from the connection.
// If a messageChan is supplied and the current message type is not
// a control message, the message is sent to that channel.
func (c *ManagedConnection) read() error {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()

	if c.connection == nil {
		return ErrConnectionNotEstablished
	}

	c.readerLock.Lock()
	defer c.readerLock.Unlock()

	c.connection.SetReadDeadline(time.Now().Add(pongTimeout))

	header, reader, err := c.connection.NextReader()
	messageType := header.OpCode
	if err != nil {
		return err
	}

	// Send the message to the channel if its an application level message
	// and if that channel is set.
	// TODO(markusthoemmes): Return the messageType along with the payload.
	if c.messageChan != nil && (messageType == ws.OpText || messageType == ws.OpBinary) {
		if message, _ := io.ReadAll(reader); message != nil {
			c.messageChan <- message
		}
	}

	return nil
}

func (c *ManagedConnection) write(messageType ws.OpCode, body []byte) error {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()

	if c.connection == nil {
		return ErrConnectionNotEstablished
	}

	c.writerLock.Lock()
	defer c.writerLock.Unlock()
	return c.connection.WriteMessage(messageType, body)
}

// Status checks the connection status of the webhook.
func (c *ManagedConnection) Status() error {
	c.connectionLock.RLock()
	defer c.connectionLock.RUnlock()

	if c.connection == nil {
		return ErrConnectionNotEstablished
	}
	return nil
}

// Send sends an encodable message over the websocket connection.
func (c *ManagedConnection) Send(msg interface{}) error {
	var b bytes.Buffer
	enc := gob.NewEncoder(&b)
	if err := enc.Encode(msg); err != nil {
		return err
	}

	return c.write(ws.OpBinary, b.Bytes())
}

// SendRaw sends a message over the websocket connection without performing any encoding.
func (c *ManagedConnection) SendRaw(messageType ws.OpCode, msg []byte) error {
	return c.write(messageType, msg)
}

// Shutdown closes the websocket connection.
func (c *ManagedConnection) Shutdown() error {
	c.closeOnce.Do(func() {
		close(c.closeChan)
	})

	err := c.closeConnection()
	c.processingWg.Wait()
	return err
}
