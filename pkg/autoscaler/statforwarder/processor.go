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

package statforwarder

import (
	"sync"
	"time"

	gorillawebsocket "github.com/gorilla/websocket"
	"go.uber.org/zap"

	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/websocket"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
)

// The timeout value for a Websocket connection to be established. If a connection via IP
// address can not be established within this value, we assume the Pods can not be
// accessed by IP address directly due to the network mesh.
const establishTimeout = 500 * time.Millisecond

// bucketProcessor includes the information about how to process
// the StatMessage owned by a bucket.
type bucketProcessor struct {
	logger *zap.SugaredLogger
	// The name of the bucket
	bkt string
	// holder is the HolderIdentity of a Lease for a bucket.
	holder   string
	svcDNS   string
	connLock sync.RWMutex
	// conn is the WebSocket connection to the holder pod.
	conn *websocket.ManagedConnection
	// `accept` is the function to process a StatMessage which doesn't need
	// to be forwarded.
	accept statProcessor
}

func newForwardProcessor(logger *zap.SugaredLogger, bkt, holder, podDNS, svcDNS string) *bucketProcessor {
	// First try to connect via Pod IP address synchronously. If the connection can
	// not be established within `establishTimeout`, we assume the pods can not be
	// accessed by IP address. Then try to connect via Pod IP address synchronously.
	logger.Info("Connecting to Autoscaler bucket at ", podDNS)
	c, err := websocket.NewDurableSendingConnectionGuaranteed(podDNS, establishTimeout, logger)
	if err != nil {
		logger.Info("Autoscaler pods can't be accessed by IP address. Connecting to Autoscaler bucket at ", svcDNS)
		c, _ = websocket.NewDurableSendingConnectionGuaranteed(svcDNS, establishTimeout, logger)
	}
	return &bucketProcessor{
		logger: logger,
		bkt:    bkt,
		holder: holder,
		conn:   c,
		svcDNS: svcDNS,
	}
}

func (p *bucketProcessor) getConn() *websocket.ManagedConnection {
	p.connLock.RLock()
	defer p.connLock.RUnlock()
	return p.conn
}

func (p *bucketProcessor) setConn(conn *websocket.ManagedConnection) {
	p.connLock.Lock()
	defer p.connLock.Unlock()
	p.conn = conn
}

func (p *bucketProcessor) process(sm asmetrics.StatMessage) error {
	l := p.logger.With(zap.String(logkey.Key, sm.Key.String()))
	if p.accept != nil {
		l.Debug("Accept stat as owner of bucket ", p.bkt)
		p.accept(sm)
		return nil
	}

	l.Debugf("Forward stat of bucket %s to the holder %s", p.bkt, p.holder)
	wsms := asmetrics.ToWireStatMessages([]asmetrics.StatMessage{sm})
	b, err := wsms.Marshal()
	if err != nil {
		return err
	}

	c := p.getConn()
	if c == nil {
		c, err = websocket.NewDurableSendingConnectionGuaranteed(p.svcDNS, establishTimeout, p.logger)
		if err != nil {
			return err
		}
		p.setConn(c)
	}

	return c.SendRaw(gorillawebsocket.BinaryMessage, b)
}

func (p *bucketProcessor) shutdown() {
	if c := p.getConn(); c != nil {
		c.Shutdown()
	}
}
