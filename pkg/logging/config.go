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
	"errors"

	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
)

// NewLogger creates a logger with the supplied configuration.
// If configuration is empty, a fallback configuration is used.
// If configuration cannot be used to instantiate a logger,
// the same fallback configuration is used.
func NewLogger(configJSON string) *zap.SugaredLogger {
	logger, err := newLoggerFromConfig(configJSON)
	if err == nil {
		logger.Info("Successfully created the logger.", zap.String(logkey.JSONConfig, configJSON))
		return logger.Sugar()
	}

	logger, err2 := zap.NewProduction()
	if err2 != nil {
		panic(err2)
	}

	logger.Error("Failed to parse the logging config. Falling back to default logger.", zap.Error(err), zap.String(logkey.JSONConfig, configJSON))
	return logger.Sugar()
}

func newLoggerFromConfig(configJSON string) (*zap.Logger, error) {
	if len(configJSON) == 0 {
		return nil, errors.New("empty logging configuration")
	}

	var loggingCfg zap.Config
	if err := json.Unmarshal([]byte(configJSON), &loggingCfg); err != nil {
		return nil, err
	}

	return loggingCfg.Build()
}
