/*
Copyright 2018 Google LLC

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
	"encoding/json"
	"fmt"

	"go.uber.org/zap"
)

// NewLogger creates a logger with the supplied configuration.
// If configuration is empty, a fallback configuration is used.
// If configuration cannot be used to instantiate a logger,
// the same fallback configuration is used.
func NewLogger(configJSON string) *zap.SugaredLogger {
	var loggingCfg zap.Config
	var logInitFailure string
	if len(configJSON) > 0 {
		if err := json.Unmarshal([]byte(configJSON), &loggingCfg); err != nil {
			// Failed to parse the logging configuration. Fall back to production config
			loggingCfg = zap.NewProductionConfig()
			logInitFailure = fmt.Sprintf(
				"Failed to parse the logging config. Will use default config. Parsing error: %v", err)
		}
	} else {
		loggingCfg = zap.NewProductionConfig()
	}

	rawLogger, err := loggingCfg.Build()
	if err != nil {
		panic(err)
	}

	logger := rawLogger.Sugar()
	if len(logInitFailure) > 0 {
		logger.Error(logInitFailure)
	}

	return logger
}
