/*
Copyright 2024 The Knative Authors

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

package logging

import (
	"time"

	"go.uber.org/zap/zapcore"
)

var beijingLocation *time.Location

func init() {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.UTC
	}
	beijingLocation = loc
}

// BeijingTimeEncoder encodes time in Beijing timezone with millisecond precision
// Format: 2006-01-02 15:04:05.000
func BeijingTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	localTime := t.In(beijingLocation)
	timeStr := localTime.Format("2006-01-02 15:04:05.000")
	enc.AppendString(timeStr)
}

// BeijingTimeEncoderFunc returns a time encoder function for Beijing timezone
// Format: 2006-01-02 15:04:05.000
func BeijingTimeEncoderFunc() zapcore.TimeEncoder {
	return BeijingTimeEncoder
}