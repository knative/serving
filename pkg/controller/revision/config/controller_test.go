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
	"github.com/knative/serving/pkg"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewControllerConfigwithoutQueueSideCarImage(t *testing.T) {
	c, err := NewControllerConfigFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      ControllerConfigName,
		},
	})

	if err == nil {
		t.Errorf("NewControllerConfigFromConfigMap() = %v, wanted error", c)
	}
}

func TestNewControllerConfigWithoutAutoscalerImage(t *testing.T) {
	var want = "some-image"
	c, err := NewControllerConfigFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      NetworkConfigName,
		},
		Data: map[string]string{
			queueSidecarImageKey: want,
		},
	})

	if err != nil {
		t.Errorf("NewControllerConfigFromConfigMap() = %v", err)
	}

	if c.QueueSidecarImage != want {
		t.Errorf("want %q, but got %q", want, c.QueueSidecarImage)
	}
}

func TestNewControllerConfigWithAutoscalerImage(t *testing.T) {
	var want = "some-autoscale-image"

	c, err := NewControllerConfigFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      ControllerConfigName,
		},
		Data: map[string]string{
			queueSidecarImageKey: "some-image",
			autoscalerImageKey:   want,
		},
	})

	if err != nil {
		t.Errorf("NewControllerConfigFromConfigMap() = %v", err)
	}

	if c.AutoscalerImage != want {
		t.Errorf("want %q, but got %q", want, c.AutoscalerImage)
	}
}

func TestNewControllerConfigwWithRegisteries(t *testing.T) {
	wantRegistry := map[string]struct{}{
		"ko.local": struct{}{},
		"ko.dev":   struct{}{},
	}

	c, err := NewControllerConfigFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      ControllerConfigName,
		},
		Data: map[string]string{
			queueSidecarImageKey:           "some-image",
			registriesSkippingTagResolving: "ko.local,ko.dev",
		},
	})

	if err != nil {
		t.Errorf("NewControllerConfigFromConfigMap() = %v", err)
	}

	if diff := cmp.Diff(c.RegistriesSkippingTagResolving, wantRegistry); diff != "" {
		t.Errorf("want %q, but got %q", wantRegistry, c.RegistriesSkippingTagResolving)
	}
}

func TestNewControllerConfigwWithBadRegisteries(t *testing.T) {
	want := map[string]struct{}{
		"ko.local": struct{}{},
		"":         struct{}{},
	}

	c, err := NewControllerConfigFromConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      ControllerConfigName,
		},
		Data: map[string]string{
			queueSidecarImageKey:           "some-image",
			registriesSkippingTagResolving: "ko.local,,",
		},
	})

	if err != nil {
		t.Errorf("NewControllerConfigFromConfigMap() = %v", err)
	}

	if diff := cmp.Diff(c.RegistriesSkippingTagResolving, want); diff != "" {
		t.Errorf("want %q, but got %q", want, c.RegistriesSkippingTagResolving)
	}
}

func TestControllerConfiguration(t *testing.T) {
	b, err := ioutil.ReadFile(fmt.Sprintf("testdata/config-%s.yaml", ControllerConfigName))
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
