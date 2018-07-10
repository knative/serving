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

package configmap

import (
	"log"

	corev1 "k8s.io/api/core/v1"
)

// fixedImpl provides a fixed informer-based implementation of Watcher.
type fixedImpl struct {
	cfgs map[string]*corev1.ConfigMap
}

// Asserts that fixedImpl implements Watcher.
var _ Watcher = (*fixedImpl)(nil)

// Watch implements Watcher
func (di *fixedImpl) Watch(name string, w Observer) {
	cm, ok := di.cfgs[name]
	if ok {
		w(cm)
	} else {
		log.Printf("Name %q is not found.", name)
	}
}

// Start implements Watcher
func (di *fixedImpl) Start(stopCh <-chan struct{}) error {
	return nil
}

// NewFixedWatcher returns an Watcher that exposes the fixed collection of ConfigMaps.
func NewFixedWatcher(cms ...*corev1.ConfigMap) Watcher {
	cmm := make(map[string]*corev1.ConfigMap)
	for _, cm := range cms {
		cmm[cm.Name] = cm
	}
	return &fixedImpl{cfgs: cmm}
}
