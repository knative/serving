/*
Copyright 2018 Google Inc. All Rights Reserved.
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

// logging.go contains the logic to configure and interact with the
// logging and metrics libraries.

package test

import (
	"flag"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/knative/serving/pkg/logging"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
)

const (
	// VerboseLogLevel defines verbose log level as 10
	VerboseLogLevel glog.Level = 10

	// 1 second was chosen arbitrarily
	metricViewReportingPeriod = 1 * time.Second
)

// Logger is to be used by test cases for logging and is also used
// for emitting metrics.
var Logger *zap.SugaredLogger

// ZapMetricExporter is a stats and trace exporter that logs the
// exported data to the provided (probably test specific) zap logger.
// It conforms to the view.Exporter and trace.Exporter interfaces.
type ZapMetricExporter struct {
	logger *zap.SugaredLogger
}

// ExportView will emit the view data vd (i.e. the stats that have been
// recorded) to the zap logger.
func (e *ZapMetricExporter) ExportView(vd *view.Data) {
	// We are not curretnly consuming these metrics, so for now we'll juse
	// dump the view.Data object as is.
	e.logger.Info(vd)
}

// ExportSpan will emit the trace data to the zap logger.
func (e *ZapMetricExporter) ExportSpan(vd *trace.SpanData) {
	duration := vd.EndTime.Sub(vd.StartTime)
	// We will start the log entry with `metric` to identify it as a metric for parsing
	e.logger.Infof("metric %s %d %d %s", vd.Name, vd.StartTime.UnixNano(), vd.EndTime.UnixNano(), duration)
}

func newLogger(logLevel string) *zap.SugaredLogger {
	configJSONTemplate := `{
	  "level": "%s",
	  "encoding": "console",
	  "outputPaths": ["stdout"],
	  "errorOutputPaths": ["stderr"],
	  "encoderConfig": {
	    "messageKey": "message",
			"levelKey": "level",
			"nameKey": "logger",
			"callerKey": "caller",
			"messageKey": "msg",
      "stacktraceKey": "stacktrace",
      "lineEnding": "",
      "levelEncoder": "",
      "timeEncoder": "",
      "durationEncoder": "",
      "callerEncoder": ""
	  }
	}`
	configJSON := fmt.Sprintf(configJSONTemplate, logLevel)
	l, _ := logging.NewLogger(string(configJSON), logLevel)
	return l
}

func initializeMetricExporter() {
	exporter := &ZapMetricExporter{logger: Logger}
	view.RegisterExporter(exporter)
	trace.RegisterExporter(exporter)

	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
	view.SetReportingPeriod(metricViewReportingPeriod)
}

func initializeLogger(logVerbose bool) {
	logLevel := "info"
	if logVerbose {
		// Both gLog and "go test" use -v flag. The code below is a work around so that we can still set v value for gLog
		flag.StringVar(&logLevel, "logLevel", fmt.Sprint(VerboseLogLevel), "verbose log level")
		flag.Lookup("v").Value.Set(logLevel)
		glog.Infof("Logging set to verbose mode with logLevel %d", VerboseLogLevel)

		logLevel = "debug"
	}
	Logger = newLogger(logLevel)
}
