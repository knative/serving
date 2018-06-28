/*
Copyright 2018 The Knative Authors.

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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

const completeConfig = `{
	"level": "info",
	"development": false,
	"sampling": {
		"initial": 100,
		"thereafter": 100
	},
	"outputPaths": ["stdout"],
	"errorOutputPaths": ["stderr"],
	"encoding": "json",
	"encoderConfig": {
		"timeKey": "",
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
const minimalConfigTemplate = `{
	"level": "%s",
	"outputPaths": ["stdout"],
	"errorOutputPaths": ["stderr"],
	"encoding": "json"
}`

func TestNewLoggerEmptyConfig(t *testing.T) {
	logger, err := NewLogger("", "")
	if err == nil {
		t.Error("expected error when providing empty config")
	}
	if logger == nil {
		t.Error("expected a non-nil logger")
	}
}

func TestNewLoggerInvalidJson(t *testing.T) {
	logger, err := NewLogger("some invalid JSON here", "")
	if err == nil {
		t.Error("expected error when providing invalid config")
	}
	if logger == nil {
		t.Error("expected a non-nil logger")
	}
}

func TestNewLoggerValidErrorConfig(t *testing.T) {
	// No good way to test if all the config is applied,
	// but at the minimum, we can check and see if level is getting applied.
	jsonConfig := fmt.Sprintf(minimalConfigTemplate, "error")
	logger, err := NewLogger(jsonConfig, "")
	if err != nil {
		t.Errorf("did not expect error when providing valid config with error log level: %s", err)
	}
	if logger == nil {
		t.Error("expected a non-nil logger")
	}
	if ce := logger.Desugar().Check(zap.InfoLevel, "test"); ce != nil {
		t.Error("not expected to get info logs from the logger configured with error as min threshold")
	}
	if ce := logger.Desugar().Check(zap.ErrorLevel, "test"); ce == nil {
		t.Error("expected to get error logs from the logger configured with error as min threshold")
	}
}

func TestNewLoggerValidInfoConfig(t *testing.T) {
	jsonConfig := fmt.Sprintf(minimalConfigTemplate, "info")
	logger, err := NewLogger(jsonConfig, "")
	if err != nil {
		t.Errorf("did not expect error when providing valid config with info log level: %s", err)
	}
	if logger == nil {
		t.Error("expected a non-nil logger")
	}
	if ce := logger.Desugar().Check(zap.DebugLevel, "test"); ce != nil {
		t.Error("not expected to get debug logs from the logger configured with info as min threshold")
	}
	if ce := logger.Desugar().Check(zap.InfoLevel, "test"); ce == nil {
		t.Error("expected to get info logs from the logger configured with info as min threshold")
	}
}

func TestNewLoggerOverrideInfo(t *testing.T) {
	jsonConfig := fmt.Sprintf(minimalConfigTemplate, "error")
	logger, err := NewLogger(jsonConfig, "info")
	if err != nil {
		t.Errorf("did not expect error when providing valid config with overridden info log level: %s", err)
	}
	if logger == nil {
		t.Error("expected a non-nil logger")
	}
	if ce := logger.Desugar().Check(zap.DebugLevel, "test"); ce != nil {
		t.Error("not expected to get debug logs from the logger configured with info as min threshold")
	}
	if ce := logger.Desugar().Check(zap.InfoLevel, "test"); ce == nil {
		t.Error("expected to get info logs from the logger configured with info as min threshold")
	}
}

func TestNewLoggerInvalidOverride(t *testing.T) {
	jsonConfig := fmt.Sprintf(minimalConfigTemplate, "error")
	logger, err := NewLogger(jsonConfig, "randomstring")
	if err != nil {
		t.Errorf("did not expect error when providing invalid log level override, it should just be ignored: %s", err)
	}
	if logger == nil {
		t.Error("expected a non-nil logger")
	}
	if ce := logger.Desugar().Check(zap.InfoLevel, "test"); ce != nil {
		t.Error("not expected to get info logs from the logger configured with error as min threshold")
	}
	if ce := logger.Desugar().Check(zap.ErrorLevel, "test"); ce == nil {
		t.Error("expected to get error logs from the logger configured with error as min threshold")
	}
}

func TestNewDefaultConfigMapLogger(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("Could not create temporary directory for test config: %s", err)
	}
	defer os.RemoveAll(dir)
	if err := writeConfig(dir, "loglevel.myfoo", "info"); err != nil {
		t.Fatalf("Failed to write example config to temporary dir %s: %s", dir, err)
	}
	if err := writeConfig(dir, "zap-logger-config", completeConfig); err != nil {
		t.Fatalf("Failed to write example config to temporary dir %s: %s", dir, err)
	}

	logger, err := NewDefaultConfigMapLogger(dir, "myfoo")
	if err != nil {
		t.Errorf("did not expect error when creating logger from config file: %s", err)
	}
	if logger == nil {
		t.Error("expected logger to be created successfully")
	}
}
func TestNewDefaultConfigMapLoggerInvalidUsesDefault(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("Could not create temporary directory for test config: %s", err)
	}
	defer os.RemoveAll(dir)
	if err := writeConfig(dir, "zap-logger-config", "catbus"); err != nil {
		t.Fatalf("Failed to write example config to temporary dir %s: %s", dir, err)
	}

	logger, err := NewDefaultConfigMapLogger(dir, "myfoo")
	if err == nil {
		t.Error("expected error when creating logger from invalid config file")
	}
	if logger == nil {
		t.Error("expected default logger to be created successfully")
	}
}

func writeConfig(dir, key, value string) error {
	filename := filepath.Join(dir, key)
	return ioutil.WriteFile(filename, []byte(value), 0666)
}
