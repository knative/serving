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
	"context"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
)

const (
	// connectionCheckInterval is how often to check the autoscaler connection status.
	connectionCheckInterval = 5 * time.Second
)

// RawSender sends raw byte array messages with a message type
// (implemented by gorilla/websocket.Socket).
type RawSender interface {
	SendRaw(msgType int, msg []byte) error
}

// StatusChecker checks the connection status.
type StatusChecker interface {
	Status() error
}

// AutoscalerConnectionStatusMonitor periodically checks if the autoscaler is reachable
// and emits metrics and logs accordingly.
func AutoscalerConnectionStatusMonitor(ctx context.Context, logger *zap.SugaredLogger, conn StatusChecker, mp metric.MeterProvider) {
	metrics := newStatReporterMetrics(mp)
	ticker := time.NewTicker(connectionCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := conn.Status(); err != nil {
				logger.Errorw("Autoscaler is not reachable from activator.",
					zap.Error(err))
				metrics.autoscalerReachable.Record(context.Background(), 0)
			} else {
				metrics.autoscalerReachable.Record(context.Background(), 1)
			}
		}
	}
}

// ReportStats sends any messages received on the source channel to the sink.
// The messages are sent on a goroutine to avoid blocking, which means that
// messages may arrive out of order.
func ReportStats(logger *zap.SugaredLogger, sink RawSender, source <-chan []asmetrics.StatMessage, mp metric.MeterProvider) {
	metrics := newStatReporterMetrics(mp)
	for sms := range source {
		go func(sms []asmetrics.StatMessage) {
			wsms := asmetrics.ToWireStatMessages(sms)
			b, err := wsms.Marshal()
			if err != nil {
				logger.Errorw("Error while marshaling stats", zap.Error(err))
				return
			}

			if err := sink.SendRaw(websocket.BinaryMessage, b); err != nil {
				logger.Errorw("Autoscaler is not reachable from activator. Stats were not sent.",
					zap.Error(err),
					zap.Int("stat_message_count", len(sms)))
				metrics.autoscalerReachable.Record(context.Background(), 0)
			} else {
				metrics.autoscalerReachable.Record(context.Background(), 1)
			}
		}(sms)
	}
}
