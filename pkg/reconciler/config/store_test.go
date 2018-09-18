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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/knative/pkg/configmap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/knative/pkg/logging/testing"
)

func TestStoreWatchConfigs(t *testing.T) {
	constructor := func(c *corev1.ConfigMap) (interface{}, error) {
		return c.Name, nil
	}

	store := NewUntypedStore(
		"name",
		TestLogger(t),
		Constructors{
			"config-name-1": constructor,
			"config-name-2": constructor,
		},
	)

	watcher := &mockWatcher{}
	store.WatchConfigs(watcher)

	want := []string{
		"config-name-1",
		"config-name-2",
	}

	got := watcher.watches

	if diff := cmp.Diff(want, got, sortStrings); diff != "" {
		t.Errorf("Unexpected configmap watches (-want, +got): %v", diff)
	}
}

func TestStoreConfigChange(t *testing.T) {
	constructor := func(c *corev1.ConfigMap) (interface{}, error) {
		return c.Name, nil
	}

	store := NewUntypedStore(
		"name",
		TestLogger(t),
		Constructors{
			"config-name-1": constructor,
			"config-name-2": constructor,
		},
	)

	store.OnConfigChanged(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config-name-1",
		},
	})

	result := store.UntypedLoad("config-name-1")

	if diff := cmp.Diff(result, "config-name-1"); diff != "" {
		t.Errorf("Expected loaded value diff: %s", diff)
	}

	result = store.UntypedLoad("config-name-2")

	if diff := cmp.Diff(result, nil); diff != "" {
		t.Errorf("Unexpected loaded value diff: %s", diff)
	}

	store.OnConfigChanged(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config-name-2",
		},
	})

	result = store.UntypedLoad("config-name-2")

	if diff := cmp.Diff(result, "config-name-2"); diff != "" {
		t.Errorf("Expected loaded value diff: %s", diff)
	}
}

func TestStoreFailedFirstConversionCrashes(t *testing.T) {
	if os.Getenv("CRASH") == "1" {
		constructor := func(c *corev1.ConfigMap) (interface{}, error) {
			return nil, errors.New("failure")
		}

		store := NewUntypedStore("name", TestLogger(t),
			Constructors{"config-name-1": constructor},
		)

		store.OnConfigChanged(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "config-name-1",
			},
		})
		return
	}

	cmd := exec.Command(os.Args[0], fmt.Sprintf("-test.run=%s", t.Name()))
	cmd.Env = append(os.Environ(), "CRASH=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("process should have exited with status 1 - err %v", err)
}

func TestStoreFailedUpdate(t *testing.T) {
	induceConstructorFailure := false

	constructor := func(c *corev1.ConfigMap) (interface{}, error) {
		if induceConstructorFailure {
			return nil, errors.New("failure")
		}

		return time.Now().String(), nil
	}

	store := NewUntypedStore("name", TestLogger(t),
		Constructors{"config-name-1": constructor},
	)

	store.OnConfigChanged(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config-name-1",
		},
	})

	firstLoad := store.UntypedLoad("config-name-1")

	induceConstructorFailure = true
	store.OnConfigChanged(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "config-name-1",
		},
	})

	secondLoad := store.UntypedLoad("config-name-1")

	if diff := cmp.Diff(firstLoad, secondLoad); diff != "" {
		t.Errorf("Expected loaded value to remain the same dff: %s", diff)
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
