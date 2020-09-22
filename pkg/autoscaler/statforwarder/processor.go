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
	"time"

	gorillawebsocket "github.com/gorilla/websocket"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/wait"

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
	// holder is the HolderIdentity for a bucket from the Lease.
	holder string
	// conn is the WebSocket connection to the holder pod.
	conn *websocket.ManagedConnection
	// `accept` is the function to process a StatMessage which doesn't need
	// to be forwarded.
	accept statProcessor
}

func newForwardProcessor(logger *zap.SugaredLogger, bkt, holder, podDNS, svcDNS string) *bucketProcessor {
	// First try to connect via Pod IP address synchronously. If the connection can
	// not be established within `establishTimeout`, we assume the pods can not be
	// accessed by IP address. Then try to connect via Pod IP address asynchronously
	// to avoid nil check.
	logger.Infof("Connecting to Autoscaler bucket at ", podDNS)
	c, err := newConnection(podDNS, logger)
	if err != nil {
		logger.Info("Autoscaler pods can't be accessed by IP address. Connecting to Autoscaler bucket at ", svcDNS)
		c = websocket.NewDurableSendingConnection(svcDNS, logger)
	}
	return &bucketProcessor{
		logger: logger,
		bkt:    bkt,
		holder: holder,
		conn:   c,
	}
}

func (p *bucketProcessor) process(sm asmetrics.StatMessage) error {
	l := p.logger.With(zap.String("revision", sm.Key.String()))
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

	return p.conn.SendRaw(gorillawebsocket.BinaryMessage, b)
}

func (p *bucketProcessor) shutdown() {
	if p.conn != nil {
		p.conn.Shutdown()
	}
}

// TODO(yanweiguo): use websocket.NewDurableSendingConnectionGuaranteed instead once knative.dev/pkg
// is unpined.
func newConnection(dns string, logger *zap.SugaredLogger) (*websocket.ManagedConnection, error) {
	c := websocket.NewDurableSendingConnection(dns, logger)
	if err := wait.PollImmediate(10*time.Millisecond, establishTimeout, func() (bool, error) {
		return c.Status() == nil, nil
	}); err != nil {
		c.Shutdown()
		return nil, websocket.ErrConnectionNotEstablished
	}

	return c, nil
}
