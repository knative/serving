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
	gorillawebsocket "github.com/gorilla/websocket"
	"go.uber.org/zap"

	"knative.dev/pkg/websocket"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
)

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

	if err := p.conn.SendRaw(gorillawebsocket.BinaryMessage, b); err != nil {
		return err
	}

	return nil
}

func (p *bucketProcessor) shutdown() {
	if p.conn != nil {
		p.conn.Shutdown()
	}
}
