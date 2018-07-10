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
	"testing"

	"github.com/ghodss/yaml"
	"github.com/knative/serving/pkg/system"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestNewConfigNoEntry(t *testing.T) {
	c := NewConfigFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      "config-logging",
		},
	})
	if got, want := c.LoggingConfig, ""; got != want {
		t.Errorf("LoggingConfig = %v, want %v", got, want)
	}
	if got, want := len(c.LoggingLevel), 0; got != want {
		t.Errorf("len(LoggingLevel) = %v, want %v", got, want)
	}
}

func TestNewConfig(t *testing.T) {
	wantCfg := "{\"level\": \"error\",\n\"outputPaths\": [\"stdout\"],\n\"errorOutputPaths\": [\"stderr\"],\n\"encoding\": \"json\"}"
	wantLevel := "info"
	c := NewConfigFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      "config-logging",
		},
		Data: map[string]string{
			"zap-logger-config":   wantCfg,
			"loglevel.queueproxy": wantLevel,
		},
	})
	if got := c.LoggingConfig; got != wantCfg {
		t.Errorf("LoggingConfig = %v, want %v", got, wantCfg)
	}
	if got := c.LoggingLevel["queueproxy"]; got != wantLevel {
		t.Errorf("LoggingLevel[queueproxy] = %v, want %v", got, wantLevel)
	}
}

func TestOurConfig(t *testing.T) {
	b, err := ioutil.ReadFile(fmt.Sprintf("testdata/%s.yaml", ConfigName))
	if err != nil {
		t.Errorf("ReadFile() = %v", err)
	}
	var cm corev1.ConfigMap
	if err := yaml.Unmarshal(b, &cm); err != nil {
		t.Errorf("yaml.Unmarshal() = %v", err)
	}
	if cfg := NewConfigFromConfigMap(&cm); cfg == nil {
		t.Errorf("NewConfigFromConfigMap() = %v, want non-nil", cfg)
	}
}
