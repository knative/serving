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
	"errors"
	"sync"

	corev1 "k8s.io/api/core/v1"
	informers "k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// defaultImpl provides a default informer-based implementation of Watcher.
type defaultImpl struct {
	sif      informers.SharedInformerFactory
	informer corev1informers.ConfigMapInformer
	ns       string

	m        sync.Mutex
	watchers map[string][]Observer
}

// Asserts that defaultImpl implements Watcher.
var _ Watcher = (*defaultImpl)(nil)

// Watch implements Watcher
func (di *defaultImpl) Watch(name string, w Observer) {
	di.m.Lock()
	defer di.m.Unlock()

	if di.watchers == nil {
		di.watchers = make(map[string][]Observer)
	}

	wl, _ := di.watchers[name]
	di.watchers[name] = append(wl, w)
}

// Start implements Watcher
func (di *defaultImpl) Start(stopCh <-chan struct{}) error {
	di.informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    di.addConfigMapEvent,
		UpdateFunc: di.updateConfigMapEvent,
	})

	// Start the shared informer factory.
	go di.sif.Start(stopCh)

	// Wait until it has been synced.
	if ok := cache.WaitForCacheSync(stopCh, di.informer.Informer().HasSynced); !ok {
		return errors.New("Error waiting for ConfigMap informer to sync.")
	}

	// Make sure that our informers has all of the objects that we expect to exist.
	di.m.Lock()
	defer di.m.Unlock()
	for k := range di.watchers {
		_, err := di.informer.Lister().ConfigMaps(di.ns).Get(k)
		if err != nil {
			return err
		}
	}

	return nil
}

func (di *defaultImpl) addConfigMapEvent(obj interface{}) {
	// If the ConfigMap update is outside of our namespace, then quickly disregard it.
	configMap := obj.(*corev1.ConfigMap)
	if configMap.Namespace != di.ns {
		// Outside of our namespace.
		// This shouldn't happen due to our filtered informer.
		return
	}

	// Within our namespace, take the lock and see if there are any registered watchers.
	di.m.Lock()
	defer di.m.Unlock()
	wl, ok := di.watchers[configMap.Name]
	if !ok {
		return // No watchers.
	}

	// Iterate over the watchers and invoke their callbacks.
	for _, w := range wl {
		w(configMap)
	}
}

func (di *defaultImpl) updateConfigMapEvent(old, new interface{}) {
	di.addConfigMapEvent(new)
}
