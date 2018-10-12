/*
Copyright 2018 The Knative Authors

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

package testing

import (
	"context"

	"github.com/knative/pkg/configmap"
	"github.com/knative/serving/pkg/reconciler"
)

type FakeWorkQueue struct{}

func (q *FakeWorkQueue) EnqueueKey(key string)               {}
func (q *FakeWorkQueue) Enqueue(obj interface{})             {}
func (q *FakeWorkQueue) EnqueueControllerOf(obj interface{}) {}

var _ reconciler.WorkQueue = (*FakeWorkQueue)(nil)

type FakeConfigMapWatcher struct{}

func (f *FakeConfigMapWatcher) Watch(string, configmap.Observer) {}
func (f *FakeConfigMapWatcher) Start(<-chan struct{}) error {
	return nil
}

var _ configmap.Watcher = (*FakeConfigMapWatcher)(nil)

type FakeConfigStore struct{}

func (s *FakeConfigStore) ToContext(ctx context.Context) context.Context {
	return ctx
}

func (s *FakeConfigStore) WatchConfigs(w configmap.Watcher) {}

var _ reconciler.ConfigStore = (*FakeConfigStore)(nil)
