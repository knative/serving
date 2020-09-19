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

	// podAddressable indicates whether pod is accessible with IP address.
	podAddressable bool
	svcDNS         string
	// podConn is the WebSocket connection to the holder pod with pod IP.
	podConn *websocket.ManagedConnection
	// svcConn is the WebSocket connection to the holder pod with bucket Service.
	svcConn *websocket.ManagedConnection

	// `accept` is the function to process a StatMessage which doesn't need
	// to be forwarded.
	accept statProcessor
}

func newForwardProcessor(logger *zap.SugaredLogger, bkt, holder, podDNS, svcDNS string) *bucketProcessor {
	logger.Infof("Connecting to Autoscaler bucket at %s and %s.", podDNS, svcDNS)
	// Initial with `podAddressable` true and a connection via IP address only.
	return &bucketProcessor{
		logger:         logger,
		bkt:            bkt,
		holder:         holder,
		podAddressable: true,
		podConn:        websocket.NewDurableSendingConnection(podDNS, logger),
		svcDNS:         svcDNS,
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

	if p.podAddressable && p.podConn.SendRaw(gorillawebsocket.BinaryMessage, b) == nil {
		// Pod is accessible via IP address, close the connection via SVC.
		if p.svcConn != nil {
			p.svcConn.Shutdown()
			p.svcConn = nil
		}
		return nil
	}

	if p.svcConn == nil {
		p.logger.Info("Connecting to Autoscaler bucket via service ", p.svcDNS)
		p.svcConn = websocket.NewDurableSendingConnection(p.svcDNS, p.logger)
	}

	if err := p.svcConn.SendRaw(gorillawebsocket.BinaryMessage, b); err != nil {
		// Sending via IP address and SVC both fail, return the error.
		return err
	}

	// Pod is accessible via SVC only, mark podAddressable false and close the connection via Pod IP.
	if p.podAddressable {
		p.logger.Info("Autoscaler pods can't be accessed by IP address")
		p.podAddressable = false
		if p.podConn != nil {
			p.podConn.Shutdown()
			p.podConn = nil
		}
	}

	return nil
}

func (p *bucketProcessor) shutdown() {
	if p.svcConn != nil {
		p.svcConn.Shutdown()
	}
	if p.podConn != nil {
		p.podConn.Shutdown()
	}
}
