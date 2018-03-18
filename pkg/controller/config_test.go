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

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
)

func TestNewConfigMissingConfigMap(t *testing.T) {
	_, err := NewConfig(fakekubeclientset.NewSimpleClientset())
	if err == nil {
		t.Error("Expect an error when ConfigMap not exists")
	}
}

func TestNewConfigMissingDomainSuffix(t *testing.T) {
	kubeClient := fakekubeclientset.NewSimpleClientset()
	kubeClient.CoreV1().ConfigMaps(elaNamespace).Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: elaNamespace,
			Name:      GetElaConfigMapName(),
		},
	})
	_, err := NewConfig(kubeClient)
	if err == nil {
		t.Error("Expect an error when config file not in right format")
	}
}

func TestNewConfig(t *testing.T) {
	kubeClient := fakekubeclientset.NewSimpleClientset()
	expectedSuffix := "test-domain.foo.com"
	kubeClient.CoreV1().ConfigMaps(elaNamespace).Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: elaNamespace,
			Name:      GetElaConfigMapName(),
		},
		Data: map[string]string{
			domainSuffixKey: expectedSuffix,
		},
	})
	c, err := NewConfig(kubeClient)
	if err != nil {
		t.Error("Unexpected error %v", err)
	}
	if c.DomainSuffix != expectedSuffix {
		t.Errorf("expected domain suffix %q, got %q", expectedSuffix, c.DomainSuffix)
	}
}
