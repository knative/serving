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

	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
)

const (
	ConfigName = "config-logging"
)

// NewLogger creates a logger with the supplied configuration.
// If configuration is empty, a fallback configuration is used.
// If configuration cannot be used to instantiate a logger,
// the same fallback configuration is used.
func NewLogger(configJSON string, levelOverride string) *zap.SugaredLogger {
	logger, err := newLoggerFromConfig(configJSON, levelOverride)
	if err == nil {
		return logger.Sugar()
	}

	logger, err2 := zap.NewProduction()
	if err2 != nil {
		panic(err2)
	}

	logger.Error("Failed to parse the logging config. Falling back to default logger.",
		zap.Error(err), zap.String(logkey.JSONConfig, configJSON))
	return logger.Sugar()
}

// NewLoggerFromConfig creates a logger using the provided Config
func NewLoggerFromConfig(config *Config, name string) *zap.SugaredLogger {
	return NewLogger(config.LoggingConfig, config.LoggingLevel[name]).Named(name)
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
		if err := level.Set(levelOverride); err == nil {
			loggingCfg.Level = zap.NewAtomicLevelAt(level)
		}
	}

	logger, err := loggingCfg.Build()
	if err != nil {
		return nil, err
	}

	logger.Info("Successfully created the logger.", zap.String(logkey.JSONConfig, configJSON))
	logger.Sugar().Infof("Logging level set to %v", loggingCfg.Level)
	return logger, nil
}

// Config contains the configuration defined in the logging ConfigMap.
type Config struct {
	LoggingConfig string
	LoggingLevel  map[string]string
}

// NewConfigFromMap creates a LoggingConfig from the supplied map
func NewConfigFromMap(data map[string]string) *Config {
	lc := &Config{}
	if zlc, ok := data["zap-logger-config"]; ok {
		lc.LoggingConfig = zlc
	}
	lc.LoggingLevel = make(map[string]string)
	for _, component := range []string{"queueproxy", "webhook", "activator", "autoscaler"} {
		if ll, ok := data["loglevel."+component]; ok {
			lc.LoggingLevel[component] = ll
		}
	}
	return lc
}

// NewConfigFromConfigMap creates a LoggingConfig from the supplied ConfigMap
func NewConfigFromConfigMap(configMap *corev1.ConfigMap) *Config {
	return NewConfigFromMap(configMap.Data)
}
