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
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	. "knative.dev/pkg/configmap/testing"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
	_ "knative.dev/pkg/system/testing"
)

func TestNewConfigNoEntry(t *testing.T) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "knative-something",
			Name:      "config-logging",
		},
	}
	cm, err := logging.NewConfigFromConfigMap(configMap)
	if err != nil {
		t.Errorf("Expected no errors. got: %v", err)
	}

	c, err := logging.NewConfigFromMap(configMap.Data)
	if err != nil {
		t.Errorf("Expected no errors. got: %v", err)
	}

	if diff := cmp.Diff(cm, c); diff != "" {
		t.Fatalf("Config mismatch: diff(-want,+got):\n%s", diff)
	}
	if got := c.LoggingConfig; got == "" {
		t.Error("LoggingConfig = empty, want not empty")
	}
	if got, want := len(c.LoggingLevel), 0; got != want {
		t.Errorf("len(LoggingLevel) = %d, want %d", got, want)
	}
	for _, component := range []string{"controller", "queueproxy", "webhook", "activator", "autoscaler"} {
		if got, want := c.LoggingLevel[component], zap.InfoLevel; got != want {
			t.Errorf("LoggingLevel[%s] = %q, want %q", component, got, want)
		}
	}
}

func TestNewConfig(t *testing.T) {
	wantCfg := "{\"level\": \"error\",\n\"outputPaths\": [\"stdout\"],\n\"errorOutputPaths\": [\"stderr\"],\n\"encoding\": \"json\"}"
	wantLevel := zapcore.InfoLevel
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      "config-logging",
		},
		Data: map[string]string{
			"zap-logger-config":   wantCfg,
			"loglevel.queueproxy": wantLevel.String(),
		},
	}

	cm, err := logging.NewConfigFromConfigMap(configMap)
	if err != nil {
		t.Errorf("Expected no errors. got: %v", err)
	}
	c, err := logging.NewConfigFromMap(configMap.Data)
	if err != nil {
		t.Errorf("Expected no errors. got: %v", err)
	}
	if diff := cmp.Diff(cm, c); diff != "" {
		t.Errorf("Config mismatch: diff(-want,+got):\n%s", diff)
	}
	if got := c.LoggingConfig; got != wantCfg {
		t.Errorf("LoggingConfig = %v, want %v", got, wantCfg)
	}
	if got := c.LoggingLevel["queueproxy"]; got != wantLevel {
		t.Errorf("LoggingLevel[queueproxy] = %v, want %v", got, wantLevel)
	}
}

func TestOurConfig(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, logging.ConfigMapName())

	if cfg, err := logging.NewConfigFromConfigMap(cm); err != nil {
		t.Errorf("Expected no errors. got: %v", err)
	} else if cfg == nil {
		t.Errorf("NewConfigFromConfigMap(actual) = %v, want non-nil", cfg)
	}

	if cfg, err := logging.NewConfigFromConfigMap(example); err != nil {
		t.Errorf("Expected no errors. got: %v", err)
	} else if cfg == nil {
		t.Errorf("NewConfigFromConfigMap(example) = %v, want non-nil", cfg)
	}
}

func TestNewLoggerFromConfig(t *testing.T) {
	c, _, _ := getTestConfig(t)
	_, atomicLevel := logging.NewLoggerFromConfig(c, "queueproxy")
	if atomicLevel.Level() != zapcore.DebugLevel {
		t.Errorf("logger level wanted: DebugLevel, got: %v", atomicLevel)
	}
}

func TestEmptyLevel(t *testing.T) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      "config-logging",
		},
		Data: map[string]string{
			"zap-logger-config":   "{\"level\": \"error\",\n\"outputPaths\": [\"stdout\"],\n\"errorOutputPaths\": [\"stderr\"],\n\"encoding\": \"json\"}",
			"loglevel.queueproxy": "",
		},
	}
	cm, err := logging.NewConfigFromConfigMap(configMap)
	if err != nil {
		t.Errorf("Expected no errors, got: %v", err)
	}

	c, err := logging.NewConfigFromMap(configMap.Data)
	if err != nil {
		t.Errorf("Expected no errors, got: %v", err)
	}

	if diff := cmp.Diff(cm, c); diff != "" {
		t.Errorf("Config mismatch: diff(-want,+got):\n%s", diff)
	}

	if got, want := c.LoggingLevel["queueproxy"], zapcore.InfoLevel; got != want {
		t.Errorf("LoggingLevel[queueproxy] = %v, want: %v", got, want)
	}
}

func TestInvalidLevel(t *testing.T) {
	wantCfg := "{\"level\": \"error\",\n\"outputPaths\": [\"stdout\"],\n\"errorOutputPaths\": [\"stderr\"],\n\"encoding\": \"json\"}"
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      "config-logging",
		},
		Data: map[string]string{
			"zap-logger-config":   wantCfg,
			"loglevel.queueproxy": "invalid",
		},
	}
	cm, err := logging.NewConfigFromConfigMap(configMap)
	if err == nil {
		t.Error("Expected errors. got nothing")
	}

	c, err := logging.NewConfigFromMap(configMap.Data)
	if err == nil {
		t.Error("Expected errors. got nothing")
	}

	if diff := cmp.Diff(cm, c); diff != "" {
		t.Errorf("Config mismatch: diff(-want,+got):\n%s", diff)
	}
}

func getTestConfig(t *testing.T) (*logging.Config, string, string) {
	wantCfg := "{\"level\": \"error\",\n\"outputPaths\": [\"stdout\"],\n\"errorOutputPaths\": [\"stderr\"],\n\"encoding\": \"json\"}"
	wantLevel := "debug"
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      "config-logging",
		},
		Data: map[string]string{
			"zap-logger-config":   wantCfg,
			"loglevel.queueproxy": wantLevel,
		},
	}
	cm, err := logging.NewConfigFromConfigMap(configMap)
	if err != nil {
		t.Errorf("err: %s", err)
	}
	c, err := logging.NewConfigFromMap(configMap.Data)
	if err != nil {
		t.Errorf("err: %s", err)
	}
	if diff := cmp.Diff(cm, c); diff != "" {
		t.Errorf("Config mismatch: diff(-want,+got):\n%s", diff)
	}
	return c, wantCfg, wantLevel
}

func TestUpdateLevelFromConfigMap(t *testing.T) {
	logger, atomicLevel := logging.NewLogger("", "debug")
	want := zapcore.DebugLevel
	if atomicLevel.Level() != zapcore.DebugLevel {
		t.Fatalf("Expected initial logger level to %v, got: %v", want, atomicLevel.Level())
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace(),
			Name:      "config-logging",
		},
		Data: map[string]string{
			"zap-logger-config":   "",
			"loglevel.controller": "panic",
		},
	}

	tests := []struct {
		setLevel  string
		wantLevel zapcore.Level
	}{
		{"info", zapcore.InfoLevel},
		{"error", zapcore.ErrorLevel},
		{"invalid", zapcore.ErrorLevel},
		{"debug", zapcore.DebugLevel},
		{"debug", zapcore.DebugLevel},
	}

	u := logging.UpdateLevelFromConfigMap(logger, atomicLevel, "controller")
	for _, tt := range tests {
		cm.Data["loglevel.controller"] = tt.setLevel
		u(cm)
		if atomicLevel.Level() != tt.wantLevel {
			t.Errorf("Invalid logging level. want: %v, got: %v", tt.wantLevel, atomicLevel.Level())
		}
	}
}
