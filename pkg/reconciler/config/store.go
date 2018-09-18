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
	"reflect"
	"sync/atomic"

	"github.com/knative/pkg/configmap"
	corev1 "k8s.io/api/core/v1"
)

type Logger interface {
	Infof(string, ...interface{})
	Fatalf(string, ...interface{})
	Errorf(string, ...interface{})
}

type Constructors map[string]interface{}

type UntypedStore struct {
	name   string
	logger Logger

	storages     map[string]*atomic.Value
	constructors map[string]reflect.Value
}

func NewUntypedStore(
	name string,
	logger Logger,
	constructors Constructors) *UntypedStore {

	store := &UntypedStore{
		name:         name,
		logger:       logger,
		storages:     make(map[string]*atomic.Value),
		constructors: make(map[string]reflect.Value),
	}

	for configName, constructor := range constructors {
		store.registerConfig(configName, constructor)
	}

	return store
}

func (s *UntypedStore) registerConfig(name string, constructor interface{}) {
	cType := reflect.TypeOf(constructor)

	if cType.Kind() != reflect.Func {
		panic("config constructor must be a function")
	}

	if cType.NumIn() != 1 || cType.In(0) != reflect.TypeOf(&corev1.ConfigMap{}) {
		panic("config constructor must be of the type func(*k8s.io/api/core/v1/ConfigMap) (..., error)")
	}

	errorType := reflect.TypeOf((*error)(nil)).Elem()

	if cType.NumOut() != 2 || !cType.Out(1).Implements(errorType) {
		panic("config constructor must be of the type func(*k8s.io/api/core/v1/ConfigMap) (..., error)")
	}

	s.storages[name] = &atomic.Value{}
	s.constructors[name] = reflect.ValueOf(constructor)
}

func (s *UntypedStore) WatchConfigs(w configmap.Watcher) {
	for configMapName, _ := range s.constructors {
		w.Watch(configMapName, s.OnConfigChanged)
	}
}

func (s *UntypedStore) UntypedLoad(name string) interface{} {
	storage := s.storages[name]
	return storage.Load()
}

func (s *UntypedStore) OnConfigChanged(c *corev1.ConfigMap) {
	name := c.ObjectMeta.Name

	storage := s.storages[name]
	constructor := s.constructors[name]

	inputs := []reflect.Value{
		reflect.ValueOf(c),
	}

	outputs := constructor.Call(inputs)
	result := outputs[0].Interface()
	errVal := outputs[1]

	if !errVal.IsNil() {
		err := errVal.Interface()
		if storage.Load() != nil {
			s.logger.Errorf("Error updating %s config %q: %q", s.name, name, err)
		} else {
			s.logger.Fatalf("Error initializing %s config %q: %q", s.name, name, err)
		}
		return
	}

	s.logger.Infof("%s config %q config was added or updated: %v", s.name, name, result)
	storage.Store(result)
}
