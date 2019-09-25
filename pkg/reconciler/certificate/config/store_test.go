/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

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
	. "knative.dev/pkg/configmap/testing"
	. "knative.dev/pkg/logging/testing"
)

func TestStoreLoadWithContext(t *testing.T) {
	store := NewStore(TestLogger(t))

	certManagerConfig := ConfigMapFromTestFile(t, CertManagerConfigName)
	store.OnConfigChanged(certManagerConfig)
	config := FromContext(store.ToContext(context.Background()))

	expected, _ := NewCertManagerConfigFromConfigMap(certManagerConfig)
	if diff := cmp.Diff(expected, config.CertManager); diff != "" {
		t.Errorf("Unexpected CertManager config (-want, +got): %v", diff)
	}
}

func TestStoreImmutableConfig(t *testing.T) {
	store := NewStore(TestLogger(t))
	store.OnConfigChanged(ConfigMapFromTestFile(t, CertManagerConfigName))
	config := store.Load()

	config.CertManager.IssuerRef.Kind = "newKind"
	newConfig := store.Load()
	if newConfig.CertManager.IssuerRef.Kind == "newKind" {
		t.Error("CertManager config is not immutable")
	}
}
