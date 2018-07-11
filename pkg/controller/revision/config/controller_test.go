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

	"github.com/google/go-cmp/cmp"
	"github.com/knative/serving/pkg/system"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var noSidecarImage = ""

func TestControllerConfiguration(t *testing.T) {
	b, err := ioutil.ReadFile(fmt.Sprintf("testdata/%s.yaml", ControllerConfigName))
	if err != nil {
		t.Errorf("ReadFile() = %v", err)
	}
	var cm corev1.ConfigMap
	if err := yaml.Unmarshal(b, &cm); err != nil {
		t.Errorf("yaml.Unmarshal() = %v", err)
	}
	if _, err := NewControllerConfigFromConfigMap(&cm); err != nil {
		t.Errorf("NewControllerConfigFromConfigMap() = %v", err)
	}
}

var configTests = []struct {
	name           string
	wantErr        bool
	wantController interface{}
	config         *corev1.ConfigMap
}{{
	"controller configuration with bad registries",
	false,
	&Controller{
		RegistriesSkippingTagResolving: map[string]struct{}{
			"ko.local": struct{}{},
			"":         struct{}{},
		},
		QueueSidecarImage: noSidecarImage,
	},
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      ControllerConfigName,
		},
		Data: map[string]string{
			queueSidecarImageKey:           noSidecarImage,
			registriesSkippingTagResolving: "ko.local,,",
		},
	}}, {
	"controller configuration with registries",
	false,
	&Controller{
		RegistriesSkippingTagResolving: map[string]struct{}{
			"ko.dev":   struct{}{},
			"ko.local": struct{}{},
		},
		QueueSidecarImage: noSidecarImage,
	},
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      ControllerConfigName,
		},
		Data: map[string]string{
			queueSidecarImageKey:           noSidecarImage,
			registriesSkippingTagResolving: "ko.local,ko.dev",
		},
	},
}, {
	"controller with autoscaler images",
	false,
	&Controller{
		QueueSidecarImage:              noSidecarImage,
		AutoscalerImage:                "autoscale-image",
		RegistriesSkippingTagResolving: map[string]struct{}{},
	},
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      ControllerConfigName,
		},
		Data: map[string]string{
			queueSidecarImageKey: noSidecarImage,
			autoscalerImageKey:   "autoscale-image",
		},
	},
}, {
	"controller with no side car image",
	true,
	(*Controller)(nil),
	&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: system.Namespace,
			Name:      ControllerConfigName,
		},
		Data: map[string]string{},
	},
}}

func TestConfig(t *testing.T) {
	for _, tt := range configTests {
		actualController, err := NewControllerConfigFromConfigMap(tt.config)

		if (err != nil) != tt.wantErr {
			t.Fatalf("Test: %q; NewControllerConfigFromConfigMap() error = %v, WantErr %v", tt.name, err, tt.wantErr)
		}

		if diff := cmp.Diff(actualController, tt.wantController); diff != "" {
			t.Fatalf("Test: %q; want %v, but got %v", tt.name, tt.wantController, actualController)
		}
	}
}
