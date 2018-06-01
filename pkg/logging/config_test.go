/*
Copyright 2018 Google LLC.

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
	"testing"

	"go.uber.org/zap"
)

func TestNewLogger(t *testing.T) {
	logger := NewLogger("", "")
	if logger == nil {
		t.Error("expected a non-nil logger")
	}

	logger = NewLogger("some invalid JSON here", "")
	if logger == nil {
		t.Error("expected a non-nil logger")
	}

	// No good way to test if all the config is applied,
	// but at the minimum, we can check and see if level is getting applied.
	logger = NewLogger("{\"level\": \"error\", \"outputPaths\": [\"stdout\"],\"errorOutputPaths\": [\"stderr\"],\"encoding\": \"json\"}", "")
	if logger == nil {
		t.Error("expected a non-nil logger")
	}
	if ce := logger.Desugar().Check(zap.InfoLevel, "test"); ce != nil {
		t.Error("not expected to get info logs from the logger configured with error as min threshold")
	}
	if ce := logger.Desugar().Check(zap.ErrorLevel, "test"); ce == nil {
		t.Error("expected to get error logs from the logger configured with error as min threshold")
	}

	logger = NewLogger("{\"level\": \"info\", \"outputPaths\": [\"stdout\"],\"errorOutputPaths\": [\"stderr\"],\"encoding\": \"json\"}", "")
	if logger == nil {
		t.Error("expected a non-nil logger")
	}
	if ce := logger.Desugar().Check(zap.DebugLevel, "test"); ce != nil {
		t.Error("not expected to get debug logs from the logger configured with info as min threshold")
	}
	if ce := logger.Desugar().Check(zap.InfoLevel, "test"); ce == nil {
		t.Error("expected to get info logs from the logger configured with info as min threshold")
	}

	// Test logging override
	logger = NewLogger("{\"level\": \"error\", \"outputPaths\": [\"stdout\"],\"errorOutputPaths\": [\"stderr\"],\"encoding\": \"json\"}", "info")
	if logger == nil {
		t.Error("expected a non-nil logger")
	}
	if ce := logger.Desugar().Check(zap.DebugLevel, "test"); ce != nil {
		t.Error("not expected to get debug logs from the logger configured with info as min threshold")
	}
	if ce := logger.Desugar().Check(zap.InfoLevel, "test"); ce == nil {
		t.Error("expected to get info logs from the logger configured with info as min threshold")
	}

	// Invalid logging override
	logger = NewLogger("{\"level\": \"error\", \"outputPaths\": [\"stdout\"],\"errorOutputPaths\": [\"stderr\"],\"encoding\": \"json\"}", "randomstring")
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
