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

package config

import (
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/knative/serving/pkg"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewObservabilityNoEntry(t *testing.T) {
	c, err := NewObservabilityFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      ObservabilityConfigName,
		},
	})
	if err != nil {
		t.Fatalf("NewObservabilityFromConfigMap() = %v", err)
	}
	if got, want := c.EnableVarLogCollection, false; got != want {
		t.Errorf("EnableVarLogCollection = %v, want %v", got, want)
	}
	if got, want := c.LoggingURLTemplate, ""; got != want {
		t.Errorf("LoggingURLTemplate = %v, want %v", got, want)
	}
}

func TestNewObservabilityNoSidecar(t *testing.T) {
	c, err := NewObservabilityFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      ObservabilityConfigName,
		},
		Data: map[string]string{
			"logging.enable-var-log-collection": "true",
		},
	})
	if err == nil {
		t.Fatalf("NewObservabilityFromConfigMap() = %v, want error", c)
	}
}

func TestNewObservability(t *testing.T) {
	wantFSI := "gcr.io/log-stuff/fluentd:latest"
	wantFSOC := "the-config"
	wantLUT := "https://logging.io"
	c, err := NewObservabilityFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      ObservabilityConfigName,
		},
		Data: map[string]string{
			"logging.enable-var-log-collection":     "true",
			"logging.fluentd-sidecar-image":         wantFSI,
			"logging.fluentd-sidecar-output-config": wantFSOC,
			"logging.revision-url-template":         wantLUT,
		},
	})
	if err != nil {
		t.Fatalf("NewObservabilityFromConfigMap() = %v", err)
	}
	if got := c.FluentdSidecarImage; got != wantFSI {
		t.Errorf("FluentdSidecarImage = %v, want %v", got, wantFSI)
	}
	if got := c.FluentdSidecarOutputConfig; got != wantFSOC {
		t.Errorf("FluentdSidecarOutputConfig = %v, want %v", got, wantFSOC)
	}
	if got := c.LoggingURLTemplate; got != wantLUT {
		t.Errorf("LoggingURLTemplate = %v, want %v", got, wantLUT)
	}
}

func TestOurObservability(t *testing.T) {
	b, err := ioutil.ReadFile(fmt.Sprintf("testdata/%s.yaml", ObservabilityConfigName))
	if err != nil {
		t.Errorf("ReadFile() = %v", err)
	}
	var cm corev1.ConfigMap
	if err := yaml.Unmarshal(b, &cm); err != nil {
		t.Errorf("yaml.Unmarshal() = %v", err)
	}
	if _, err := NewObservabilityFromConfigMap(&cm); err != nil {
		t.Errorf("NewObservabilityFromConfigMap() = %v", err)
	}
}
