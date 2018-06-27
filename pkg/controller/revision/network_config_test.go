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

package revision

import (
	"testing"

	"github.com/knative/serving/pkg"
	"github.com/knative/serving/pkg/controller"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
)

func TestNewConfigMissingConfigMap(t *testing.T) {
	_, err := NewNetworkConfig(fakekubeclientset.NewSimpleClientset())
	if err == nil {
		t.Error("Expected an error value when config map doesn't exist.")
	}
}

func TestNewConfigNoEntry(t *testing.T) {
	kubeClient := fakekubeclientset.NewSimpleClientset()
	kubeClient.CoreV1().ConfigMaps(pkg.GetServingSystemNamespace()).Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      controller.GetNetworkConfigMapName(),
		},
	})
	c, err := NewNetworkConfig(kubeClient)
	if err != nil {
		t.Errorf("Didn't expect an error but got %v", err)
	} else if len(c.IstioOutboundIPRanges) > 0 {
		t.Error("Expected an empty value when config map doesn't have the entry.")
	}
}

func TestNewConfig(t *testing.T) {
	kubeClient := fakekubeclientset.NewSimpleClientset()
	want := "10.10.10.10/12"
	kubeClient.CoreV1().ConfigMaps(pkg.GetServingSystemNamespace()).Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: pkg.GetServingSystemNamespace(),
			Name:      controller.GetNetworkConfigMapName(),
		},
		Data: map[string]string{
			IstioOutboundIPRangesKey: want,
			"bar.com":                "selector:\n  app: bar\n  version: beta",
		},
	})
	c, err := NewNetworkConfig(kubeClient)
	if err != nil {
		t.Errorf("Didn't expect an error but got %v", err)
	}
	if c.IstioOutboundIPRanges != want {
		t.Errorf("Want %v, got %v", want, c.IstioOutboundIPRanges)
	}
}
