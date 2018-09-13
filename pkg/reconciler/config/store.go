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

type UntypedStore struct {
	name   string
	logger Logger

	storages     map[string]*atomic.Value
	constructors map[string]reflect.Value
}

func NewUntypedStore(
	name string,
	logger Logger,
	constructors ...interface{}) *UntypedStore {

	store := &UntypedStore{
		name:         name,
		logger:       logger,
		storages:     make(map[string]*atomic.Value),
		constructors: make(map[string]reflect.Value),
	}

	// TODO(dprotaso) Check argument validity
	for i := 0; i < len(constructors); i = i + 2 {
		configName := constructors[i].(string)
		constructor := constructors[i+1]
		store.registerConfig(configName, constructor)
	}

	return store
}

func (s *UntypedStore) registerConfig(name string, constructor interface{}) {
	// TODO(dprotaso) assert constructor type
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

	// Safety here will be addressed by the TODO in registerConfig
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
