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
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/system"

	. "knative.dev/pkg/configmap/testing"
	"knative.dev/serving/pkg/network"
)

func TestStoreLoadWithContext(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	networkConfig := ConfigMapFromTestFile(t, network.ConfigName)
	store.OnConfigChanged(networkConfig)
	store.OnConfigChanged(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      IstioConfigName,
			Namespace: system.Namespace(),
		},
	})
	config := FromContext(store.ToContext(context.Background()))

	expectNetworkConfig, _ := network.NewConfigFromConfigMap(networkConfig)
	if diff := cmp.Diff(expectNetworkConfig, config.Network); diff != "" {
		t.Errorf("Unexpected TLS mode (-want, +got): %s", diff)
	}
}

func TestStoreImmutableConfig(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	store.OnConfigChanged(ConfigMapFromTestFile(t, network.ConfigName))
	store.OnConfigChanged(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      IstioConfigName,
			Namespace: system.Namespace(),
		},
	})

	config := store.Load()

	config.Network.HTTPProtocol = network.HTTPRedirected

	newConfig := store.Load()

	if newConfig.Network.HTTPProtocol == network.HTTPRedirected {
		t.Error("Network config is not immuable")
	}
}
