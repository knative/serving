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

package activator

import (
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"knative.dev/serving/pkg/autoscaler/metrics"
)

// RawSender sends raw byte array messages with a message type
// (implemented by gorilla/websocket.Socket).
type RawSender interface {
	SendRaw(msgType int, msg []byte) error
}

// ReportStats sends any messages received on the source channel to the sink.
// The messages are sent on a goroutine to avoid blocking, which means that
// messages may arrive out of order.
func ReportStats(logger *zap.SugaredLogger, sink RawSender, source <-chan []metrics.StatMessage) {
	for sms := range source {
		go func(sms []metrics.StatMessage) {
			wsms := metrics.ToWireStatMessages(sms)
			b, err := wsms.Marshal()
			if err != nil {
				logger.Errorw("Error while marshaling stats", zap.Error(err))
				return
			}

			if err := sink.SendRaw(websocket.BinaryMessage, b); err != nil {
				logger.Errorw("Error while sending stats", zap.Error(err))
			}
		}(sms)
	}
}
