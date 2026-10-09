/*
Copyright 2019 The Knative Authors

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
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"knative.dev/pkg/configmap"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/system"

	. "knative.dev/pkg/configmap/testing"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
)

var ignoreStuff = cmp.Options{
	cmpopts.IgnoreUnexported(resource.Quantity{}),
}

func TestStoreLoadWithContext(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	defaultsConfig := ConfigMapFromTestFile(t, DefaultsConfigName)
	featuresConfig := ConfigMapFromTestFile(t, FeaturesConfigName)
	autoscalerConfig := ConfigMapFromTestFile(t, autoscalerconfig.ConfigName)

	store.OnConfigChanged(defaultsConfig)
	store.OnConfigChanged(featuresConfig)
	store.OnConfigChanged(autoscalerConfig)

	config := FromContextOrDefaults(store.ToContext(context.Background()))

	t.Run("defaults", func(t *testing.T) {
		expected, _ := NewDefaultsConfigFromConfigMap(defaultsConfig)
		if diff := cmp.Diff(expected, config.Defaults, ignoreStuff...); diff != "" {
			t.Errorf("Unexpected defaults config (-want, +got):\n%v", diff)
		}
	})

	t.Run("features", func(t *testing.T) {
		expected, _ := NewFeaturesConfigFromConfigMap(featuresConfig)
		if diff := cmp.Diff(expected, config.Features, ignoreStuff...); diff != "" {
			t.Errorf("Unexpected features config (-want, +got):\n%v", diff)
		}
	})

	t.Run("autoscaler", func(t *testing.T) {
		expected, _ := autoscalerconfig.NewConfigFromConfigMap(autoscalerConfig)
		if diff := cmp.Diff(expected, config.Autoscaler, ignoreStuff...); diff != "" {
			t.Errorf("Unexpected autoscaler config (-want, +got):\n%v", diff)
		}
	})
}

func TestStoreLoadWithContextOrDefaults(t *testing.T) {
	defaultsConfig := ConfigMapFromTestFile(t, DefaultsConfigName)
	featuresConfig := ConfigMapFromTestFile(t, FeaturesConfigName)
	autoscalerConfig := ConfigMapFromTestFile(t, autoscalerconfig.ConfigName)
	config := FromContextOrDefaults(context.Background())

	t.Run("defaults", func(t *testing.T) {
		expected, _ := NewDefaultsConfigFromConfigMap(defaultsConfig)
		if diff := cmp.Diff(expected, config.Defaults, ignoreStuff...); diff != "" {
			t.Errorf("Unexpected defaults config (-want, +got):\n%v", diff)
		}
	})

	t.Run("features", func(t *testing.T) {
		expected, _ := NewFeaturesConfigFromConfigMap(featuresConfig)
		if diff := cmp.Diff(expected, config.Features, ignoreStuff...); diff != "" {
			t.Errorf("Unexpected features config (-want, +got):\n%v", diff)
		}
	})

	t.Run("autoscaler", func(t *testing.T) {
		expected, _ := autoscalerconfig.NewConfigFromConfigMap(autoscalerConfig)
		if diff := cmp.Diff(expected, config.Autoscaler, ignoreStuff...); diff != "" {
			t.Errorf("Unexpected autoscaler config (-want, +got):\n%v", diff)
		}
	})
}

func TestStoreImmutableConfig(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	store.OnConfigChanged(ConfigMapFromTestFile(t, DefaultsConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, FeaturesConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, autoscalerconfig.ConfigName))

	config := store.Load()

	config.Defaults.RevisionTimeoutSeconds = 1234
	config.Features.MultiContainer = Disabled
	config.Autoscaler.TargetBurstCapacity = 99

	newConfig := store.Load()

	if newConfig.Defaults.RevisionTimeoutSeconds == 1234 {
		t.Error("Defaults config is not immutable")
	}

	if newConfig.Features.MultiContainer == Disabled {
		t.Error("Features config is not immutable")
	}

	if newConfig.Autoscaler.TargetBurstCapacity == 99 {
		t.Error("Autoscaler config is not immutable")
	}
}

// mockDefaultingWatcher implements configmap.DefaultingWatcher for testing
type mockDefaultingWatcher struct {
	mu               sync.Mutex
	watchedDefaults  map[string]corev1.ConfigMap
	watchedCallbacks map[string][]configmap.Observer
}

func newMockDefaultingWatcher() *mockDefaultingWatcher {
	return &mockDefaultingWatcher{
		watchedDefaults:  make(map[string]corev1.ConfigMap),
		watchedCallbacks: make(map[string][]configmap.Observer),
	}
}

func (m *mockDefaultingWatcher) WatchWithDefault(cm corev1.ConfigMap, observers ...configmap.Observer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchedDefaults[cm.Name] = cm
	m.watchedCallbacks[cm.Name] = observers
}

func (m *mockDefaultingWatcher) Watch(name string, observers ...configmap.Observer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchedCallbacks[name] = observers
}

func (m *mockDefaultingWatcher) Start(<-chan struct{}) error {
	return nil
}

func (m *mockDefaultingWatcher) getWatchedDefault(name string) (corev1.ConfigMap, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cm, ok := m.watchedDefaults[name]
	return cm, ok
}

func (m *mockDefaultingWatcher) triggerCallback(cm *corev1.ConfigMap) {
	m.mu.Lock()
	callbacks := m.watchedCallbacks[cm.Name]
	m.mu.Unlock()

	for _, cb := range callbacks {
		cb(cm)
	}
}

// mockWatcher implements configmap.Watcher (without DefaultingWatcher) for testing
type mockWatcher struct {
	mu               sync.Mutex
	watchedNames     []string
	watchedCallbacks map[string][]configmap.Observer
}

func newMockWatcher() *mockWatcher {
	return &mockWatcher{
		watchedCallbacks: make(map[string][]configmap.Observer),
	}
}

func (m *mockWatcher) Watch(name string, observers ...configmap.Observer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchedNames = append(m.watchedNames, name)
	m.watchedCallbacks[name] = observers
}

func (m *mockWatcher) Start(<-chan struct{}) error {
	return nil
}

func (m *mockWatcher) getWatchedNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, len(m.watchedNames))
	copy(names, m.watchedNames)
	return names
}

func TestWatchConfigsWithDefaults_RegistersAllConfigMaps(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))
	watcher := newMockDefaultingWatcher()

	store.WatchConfigsWithDefaults(watcher, system.Namespace())

	expectedConfigMaps := []string{
		DefaultsConfigName,
		FeaturesConfigName,
		autoscalerconfig.ConfigName,
	}

	for _, expectedName := range expectedConfigMaps {
		cm, ok := watcher.getWatchedDefault(expectedName)
		if !ok {
			t.Fatalf("Expected ConfigMap %q to be registered with WatchWithDefault, but it was not", expectedName)
		} else if cm.Name != expectedName {
			t.Errorf("Expected ConfigMap name %q, got %q", expectedName, cm.Name)
		}
	}
}

func TestWatchConfigsWithDefaults_PassesNamespace(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))
	watcher := newMockDefaultingWatcher()
	namespace := "test-namespace"

	store.WatchConfigsWithDefaults(watcher, namespace)

	for name := range configConstructors {
		cm, ok := watcher.getWatchedDefault(name)
		if !ok {
			t.Fatalf("Expected ConfigMap %q to be registered", name)
			continue
		}
		if cm.Namespace != namespace {
			t.Errorf("Expected ConfigMap %q to have namespace %q, got %q", name, namespace, cm.Namespace)
		}
	}
}

func TestWatchConfigsWithDefaults_DefaultConfigsUpdateStore(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))
	watcher := newMockDefaultingWatcher()

	store.WatchConfigsWithDefaults(watcher, system.Namespace())

	// Simulate the defaulting watcher notifying observers with the default ConfigMaps.
	for name := range configConstructors {
		defaultCM, ok := watcher.getWatchedDefault(name)
		if !ok {
			t.Fatalf("ConfigMap %q not registered", name)
		}
		watcher.triggerCallback(&defaultCM)
	}

	// Verify the Store was updated via the callbacks
	config := store.Load()

	if config.Defaults == nil {
		t.Error("Expected Defaults to be set from callback")
	}
	if config.Features == nil {
		t.Error("Expected Features to be set from callback")
	}
	if config.Autoscaler == nil {
		t.Error("Expected Autoscaler to be set from callback")
	}
}

func TestWatchConfigsWithDefaults_RealConfigMapReplacesDefault(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))
	watcher := newMockDefaultingWatcher()

	store.WatchConfigsWithDefaults(watcher, system.Namespace())

	// Simulate the defaulting watcher first providing the default,
	// then observing the real ConfigMap.
	defaultCM, ok := watcher.getWatchedDefault(DefaultsConfigName)
	if !ok {
		t.Fatal("Expected default ConfigMap to be registered")
	}
	watcher.triggerCallback(&defaultCM)

	config1 := store.Load()
	if config1.Defaults == nil {
		t.Fatal("Expected Defaults to be set from default ConfigMap")
	}

	// Now trigger a real ConfigMap with actual values
	realConfigMap := ConfigMapFromTestFile(t, DefaultsConfigName)
	watcher.triggerCallback(realConfigMap)

	// Verify the real ConfigMap replaced the default
	config2 := store.Load()
	expectedDefaults, _ := NewDefaultsConfigFromConfigMap(realConfigMap)
	if diff := cmp.Diff(expectedDefaults, config2.Defaults, ignoreStuff...); diff != "" {
		t.Errorf("Real ConfigMap did not replace default (-want, +got):\n%v", diff)
	}
}

func TestWatchConfigsWithDefaults_FallbackToRegularWatch(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))
	watcher := newMockWatcher()

	// A watcher that does not implement DefaultingWatcher should use the existing Watch path.
	store.WatchConfigsWithDefaults(watcher, system.Namespace())

	// Verify that regular Watch was called for all ConfigMaps
	watchedNames := watcher.getWatchedNames()
	expectedConfigMaps := []string{
		DefaultsConfigName,
		FeaturesConfigName,
		autoscalerconfig.ConfigName,
	}

	if len(watchedNames) != len(expectedConfigMaps) {
		t.Errorf("Expected %d ConfigMaps to be watched, got %d", len(expectedConfigMaps), len(watchedNames))
	}

	// Verify all expected ConfigMaps were watched
	watchedMap := make(map[string]bool)
	for _, name := range watchedNames {
		watchedMap[name] = true
	}

	for _, expectedName := range expectedConfigMaps {
		if !watchedMap[expectedName] {
			t.Errorf("Expected ConfigMap %q to be watched with regular Watch, but it was not", expectedName)
		}
	}
}
