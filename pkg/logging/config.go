/*
Copyright 2018 The Knative Authors

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
	"fmt"

	"github.com/josephburnett/k8sflag/pkg/k8sflag"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// DefaultLoggingConfigPath is the path where the zap logging
// config will be written in a default Knative deployment.
const DefaultLoggingConfigPath = "/etc/config-logging"

// NewLogger creates a logger with the supplied configuration.
// If configuration is empty, a fallback configuration is used.
// If configuration cannot be used to instantiate a logger,
// the same fallback configuration is used.
func NewLogger(configJSON string, levelOverride string) (*zap.SugaredLogger, error) {
	logger, err := newLoggerFromConfig(configJSON, levelOverride)
	if err == nil {
		return logger.Sugar(), nil
	}

	logger, err2 := zap.NewProduction()
	if err2 != nil {
		panic(err2)
	}

	return logger.Sugar(), fmt.Errorf("Failed to parse the logging config. Falling back to default logger. Error: %s. Config: %s", err, configJSON)
}

// NewDefaultConfigMapLogger creates a logging object for the
// for the logLevelKey within configPath corresponding
// to the provided componentKey.
func NewDefaultConfigMapLogger(configPath string, componentKey string) (*zap.SugaredLogger, error) {
	logLevelKey := "loglevel." + componentKey

	loggingFlagSet := k8sflag.NewFlagSet(configPath)
	fmt.Println(loggingFlagSet)
	zapCfg := loggingFlagSet.String("zap-logger-config", "")
	zapLevelOverride := loggingFlagSet.String(logLevelKey, "")

	genericLogger, err := NewLogger(zapCfg.Get(), zapLevelOverride.Get())
	logger := genericLogger.Named(componentKey)
	return logger, err
}

func newLoggerFromConfig(configJSON string, levelOverride string) (*zap.Logger, error) {
	if len(configJSON) == 0 {
		return nil, errors.New("empty logging configuration")
	}

	var loggingCfg zap.Config
	if err := json.Unmarshal([]byte(configJSON), &loggingCfg); err != nil {
		return nil, err
	}

	if len(levelOverride) > 0 {
		var level zapcore.Level
		// If the levelOverride can't be parsed, we just ignore it and continue.
		if err := level.Set(levelOverride); err == nil {
			loggingCfg.Level = zap.NewAtomicLevelAt(level)
		}
	}

	logger, err := loggingCfg.Build()
	if err != nil {
		return nil, err
	}

	return logger, err
}
