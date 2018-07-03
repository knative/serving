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

package main

import (
	"testing"

	"github.com/knative/serving/pkg"
	"github.com/knative/serving/pkg/logging"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReceiveLoggingConfigMap(t *testing.T) {
	logger, atomicLevel := logging.NewLogger("", "debug")
	want := zapcore.DebugLevel
	if atomicLevel.Level() != zapcore.DebugLevel {
		t.Fatalf("Expected initial logger level to %v, got: %v", want, atomicLevel.Level())
	}

	receiveFunc := receiveLoggingConfig(logger, atomicLevel)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      "config-logging",
		},
		Data: map[string]string{
			"zap-logger-config":   "",
			"loglevel.controller": "info",
		},
	}

	for _, test := range []struct {
		l zapcore.Level
		s string
	}{
		{zapcore.InfoLevel, "info"},
		{zapcore.DebugLevel, "debug"},
		{zapcore.ErrorLevel, "error"},
		{zapcore.ErrorLevel, "invalid level"},
	} {
		cm.Data["loglevel.controller"] = test.s
		receiveFunc(cm)
		if atomicLevel.Level() != test.l {
			t.Errorf("Expected logger level to be %v, got: %v", test.l, atomicLevel.Level())
		}
	}
}
