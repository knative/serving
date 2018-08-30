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
	"errors"
	"io"
	"testing"
)

type inspectableConnection struct {
	nextReaderCalls   chan struct{}
	writeMessageCalls chan struct{}
	closeCalls        chan struct{}

	nextReaderFunc func() (int, io.Reader, error)
}

func (c *inspectableConnection) WriteMessage(messageType int, data []byte) error {
	c.writeMessageCalls <- struct{}{}
	return nil
}

func (c *inspectableConnection) NextReader() (int, io.Reader, error) {
	c.nextReaderCalls <- struct{}{}
	return c.nextReaderFunc()
}

func (c *inspectableConnection) Close() error {
	c.closeCalls <- struct{}{}
	return nil
}

func TestRetriesWhileConnect(t *testing.T) {
	want := 2
	got := 0

	spy := &inspectableConnection{
		closeCalls: make(chan struct{}, 1),
	}
	conn := newConnection(func() (RawConnection, error) {
		got++
		if got == want {
			return spy, nil
		}
		return nil, errors.New("not yet")
	})

	conn.connect()
	conn.Close()

	if got != want {
		t.Fatalf("Wanted %v retries. Got %v.", want, got)
	}
	if len(spy.closeCalls) != 1 {
		t.Fatalf("Wanted 'Close' to be called once, but got %v", len(spy.closeCalls))
	}
}

func TestSendErrorOnNoConnection(t *testing.T) {
	want := ErrConnectionNotEstablished

	conn := &ManagedConnection{}
	got := conn.Send("test")

	if got != want {
		t.Fatalf("Wanted error to be %v, but it was %v.", want, got)
	}
}

func TestSendErrorOnEncode(t *testing.T) {
	spy := &inspectableConnection{
		writeMessageCalls: make(chan struct{}, 1),
	}
	conn := newConnection(func() (RawConnection, error) {
		return spy, nil
	})
	conn.connect()
	// gob cannot encode nil values
	got := conn.Send(nil)

	if got == nil {
		t.Fatal("Expected an error but got none")
	}
	if len(spy.writeMessageCalls) != 0 {
		t.Fatalf("Expected 'WriteMessage' not to be called, but was called %v times", spy.writeMessageCalls)
	}
}

func TestSendMessage(t *testing.T) {
	spy := &inspectableConnection{
		writeMessageCalls: make(chan struct{}, 1),
	}
	conn := newConnection(func() (RawConnection, error) {
		return spy, nil
	})
	conn.connect()
	got := conn.Send("test")

	if got != nil {
		t.Fatalf("Expected no error but got: %+v", got)
	}
	if len(spy.writeMessageCalls) != 1 {
		t.Fatalf("Expected 'WriteMessage' to be called once, but was called %v times", spy.writeMessageCalls)
	}
}

func TestCloseClosesConnection(t *testing.T) {
	spy := &inspectableConnection{
		closeCalls: make(chan struct{}, 1),
	}
	conn := newConnection(func() (RawConnection, error) {
		return spy, nil
	})
	conn.connect()
	conn.Close()

	if len(spy.closeCalls) != 1 {
		t.Fatalf("Expected 'Close' to be called once, got %v", len(spy.closeCalls))
	}
}

func TestCloseIgnoresNoConnection(t *testing.T) {
	conn := &ManagedConnection{
		closeChan: make(chan struct{}, 1),
	}
	got := conn.Close()

	if got != nil {
		t.Fatalf("Expected no error, got %v", got)
	}
}

func TestDurableConnectionWhenConnectionBreaksDown(t *testing.T) {
	testConn := &inspectableConnection{
		nextReaderCalls:   make(chan struct{}, 1),
		writeMessageCalls: make(chan struct{}, 1),
		closeCalls:        make(chan struct{}, 2),

		nextReaderFunc: func() (int, io.Reader, error) {
			return 1, nil, errors.New("next reader errored")
		},
	}
	connectAttempts := make(chan struct{}, 1)
	conn := NewDurableSendingConnection(func() (RawConnection, error) {
		connectAttempts <- struct{}{}
		return testConn, nil
	})

	// the connection is constantly created, tried to read from
	// and closed because NextReader (which holds the connection
	// open) fails.
	for i := 0; i < 100; i++ {
		<-connectAttempts
		<-testConn.nextReaderCalls
		<-testConn.closeCalls
	}

	conn.Close()

	// If the close signal raced the reconnect loop, consume
	// the tokens put on the channels

	if len(connectAttempts) == 1 {
		<-connectAttempts
		<-testConn.nextReaderCalls
		<-testConn.closeCalls
	}

	// Wait for 2 Close calls (the explicit above and the implicit)
	<-testConn.closeCalls
	<-testConn.closeCalls

	if len(connectAttempts) > 1 {
		t.Fatalf("Expected at most one connection attempts, got %v", len(connectAttempts))
	}
	if len(testConn.nextReaderCalls) > 1 {
		t.Fatalf("Expected at most one calls to 'NextReader', got %v", len(testConn.nextReaderCalls))
	}
}
