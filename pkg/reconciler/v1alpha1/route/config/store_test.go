/*
Copyright 2018 The Knative Authors

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
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/knative/pkg/configmap"
	"github.com/knative/serving/pkg/system"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/knative/pkg/logging/testing"
)

func TestStoreWatchConfigs(t *testing.T) {
	watcher := &mockWatcher{}

	store := Store{Logger: TestLogger(t)}
	store.WatchConfigs(watcher)

	want := []string{
		DomainConfigName,
	}

	got := watcher.watches

	if diff := cmp.Diff(want, got, sortStrings); diff != "" {
		t.Errorf("Unexpected configmap watches (-want, +got): %v", diff)
	}
}

func TestStoreLoadWithContext(t *testing.T) {
	store := Store{Logger: TestLogger(t)}

	domainConfig := configMapFromFile(t, DomainConfigName)

	store.setDomain(domainConfig)

	config := FromContext(store.ToContext(context.Background()))

	t.Run("domain", func(t *testing.T) {
		expected, _ := NewDomainFromConfigMap(domainConfig)
		if diff := cmp.Diff(expected, config.Domain); diff != "" {
			t.Errorf("Unexpected controller config (-want, +got): %v", diff)
		}
	})
}

func TestStoreImmutableConfig(t *testing.T) {
	store := Store{Logger: TestLogger(t)}

	store.setDomain(configMapFromFile(t, DomainConfigName))

	config := store.Load()

	config.Domain.Domains = map[string]*LabelSelector{
		"mutated": nil,
	}

	newConfig := store.Load()

	if _, ok := newConfig.Domain.Domains["mutated"]; ok {
		t.Error("Domain config is not immutable")
	}
}

func TestStoreFailedUpdate(t *testing.T) {
	store := Store{Logger: TestLogger(t)}

	domainConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DomainConfigName,
			Namespace: system.Namespace,
		},
		Data: map[string]string{
			"example.com": "",
		},
	}

	store.setDomain(domainConfig)

	domainConfig.Data = map[string]string{} // default is required
	store.setDomain(domainConfig)

	config := store.Load()
	if _, ok := config.Domain.Domains["example.com"]; !ok {
		t.Errorf("Expected the update to fail")
	}
}

type mockWatcher struct {
	watches []string
}

func (w *mockWatcher) Watch(config string, o configmap.Observer) {
	w.watches = append(w.watches, config)
}

func (*mockWatcher) Start(<-chan struct{}) error { return nil }

var _ configmap.Watcher = (*mockWatcher)(nil)

var sortStrings = cmpopts.SortSlices(func(x, y string) bool {
	return x < y
})

func configMapFromFile(t *testing.T, name string) *corev1.ConfigMap {
	t.Helper()

	b, err := ioutil.ReadFile(fmt.Sprintf("testdata/%s.yaml", name))
	if err != nil {
		t.Errorf("ReadFile() = %v", err)
	}
	var cm corev1.ConfigMap
	if err := yaml.Unmarshal(b, &cm); err != nil {
		t.Errorf("yaml.Unmarshal() = %v", err)
	}

	return &cm
}
