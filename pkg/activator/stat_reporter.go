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
	gorillawebsocket "github.com/gorilla/websocket"
	"go.uber.org/zap"
	"knative.dev/serving/pkg/autoscaler/metrics"
)

// RawSender is an interface implemented by things that can send raw byte array
// messages (such as a gorilla/websocket.Socket).
type RawSender interface {
	SendRaw(msgType int, msg []byte) error
}

// ReportStats sends any messages received on the statChan to the statSink. The
// messages are sent on a goroutine to avoid blocking, which means that
// messages may arrive out of order.
func ReportStats(logger *zap.SugaredLogger, statSink RawSender, statChan <-chan []metrics.StatMessage) {
	for sms := range statChan {
		go func(sms []metrics.StatMessage) {
			wsms := metrics.ToWireStatMessages(sms)
			b, err := wsms.Marshal()
			if err != nil {
				logger.Errorw("Error while marshaling stats", zap.Error(err))
				return
			}

			if err := statSink.SendRaw(gorillawebsocket.BinaryMessage, b); err != nil {
				logger.Errorw("Error while sending stats", zap.Error(err))
			}
		}(sms)
	}
}
