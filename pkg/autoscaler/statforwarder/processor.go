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

const (
	forwardRetryTimeout  = 10 * time.Second
	forwardRetryInterval = 100 * time.Millisecond
)

// bucketProcessor includes the information about how to process
// the StatMessage owned by a bucket.
type bucketProcessor struct {
	logger *zap.SugaredLogger
	// The name of the bucket
	bkt string
	// holder is the HolderIdentity for a bucket from the Lease.
	holder string

	// podsAddressable indicates whether pod is accessible with IP address.
	podsAddressable bool
	svcDNS          string
	// podConn is the WebSocket connection to the holder pod with pod IP.
	podConn *websocket.ManagedConnection
	// svcConn is the WebSocket connection to the holder pod with bucket Service.
	svcConn *websocket.ManagedConnection

	// `accept` is the function to process a StatMessage which doesn't need
	// to be forwarded.
	accept statProcessor
}

func newForwardProcessor(logger *zap.SugaredLogger, bkt, holder, podDNS, svcDNS string) *bucketProcessor {
	logger.Info("Connecting to Autoscaler bucket at ", podDNS)
	// Initial with `podsAddressable` true and a connection via IP address only.
	return &bucketProcessor{
		logger:          logger,
		bkt:             bkt,
		holder:          holder,
		podsAddressable: true,
		podConn:         websocket.NewDurableSendingConnection(podDNS, logger),
		svcDNS:          svcDNS,
	}
}

func (p *bucketProcessor) process(sm asmetrics.StatMessage) {
	l := p.logger.With(zap.String("revision", sm.Key.String()))
	if p.accept != nil {
		l.Debug("Accept stat as owner of bucket ", p.bkt)
		p.accept(sm)
		return
	}

	l.Debugf("Forward stat of bucket %s to the holder %s", p.bkt, p.holder)
	wsms := asmetrics.ToWireStatMessages([]asmetrics.StatMessage{sm})
	b, err := wsms.Marshal()
	if err != nil {
		l.Errorw("Error while marshaling stats", zap.Error(err))
		return
	}

	var lastErr error
	if err := wait.PollImmediate(retryInterval, retryTimeout, func() (bool, error) {
		if p.podsAddressable {
			if err := p.podConn.SendRaw(gorillawebsocket.BinaryMessage, b); err == nil {
				// In this case, there's no mesh.
				if p.svcConn != nil {
					if err := p.svcConn.Shutdown(); err != nil {
						p.svcConn = nil
					}
				}
				return true, nil
			}
		}

		if p.svcConn == nil {
			p.logger.Info("Connecting to Autoscaler bucket at ", p.svcDNS)
			p.svcConn = websocket.NewDurableSendingConnection(p.svcDNS, p.logger)
		}

		err = p.svcConn.SendRaw(gorillawebsocket.BinaryMessage, b)
		if err == nil {
			// In this case, there's mesh.
			p.podsAddressable = false
			if p.podConn != nil {
				if err := p.podConn.Shutdown(); err != nil {
					p.podConn = nil
				}
			}
			return true, nil
		}

		lastErr = err
		return false, nil
	}); err != nil {
		p.logger.Warnf("Failed to foward stat within %v seconds: %v", forwardRetryTimeout.Seconds, lastErr)
	}
}

func (p *bucketProcessor) shutdown() {
	if p.svcConn != nil {
		p.svcConn.Shutdown()
	}
	if p.podConn != nil {
		p.podConn.Shutdown()
	}
}
