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

	// Guards mutations to defaultImpl fields
	m sync.Mutex

	observers map[string][]Observer
	started   bool
}

// Asserts that defaultImpl implements Watcher.
var _ Watcher = (*defaultImpl)(nil)

// Watch implements Watcher
func (di *defaultImpl) Watch(name string, w Observer) {
	di.m.Lock()
	defer di.m.Unlock()

	if di.observers == nil {
		di.observers = make(map[string][]Observer)
	}

	wl, _ := di.observers[name]
	di.observers[name] = append(wl, w)
}

// Start implements Watcher
func (di *defaultImpl) Start(stopCh <-chan struct{}) error {
	if err := di.registerCallbackAndStartInformer(stopCh); err != nil {
		return err
	}

	// Wait until it has been synced (WITHOUT holding the mutex, so callbacks happen)
	if ok := cache.WaitForCacheSync(stopCh, di.informer.Informer().HasSynced); !ok {
		return errors.New("Error waiting for ConfigMap informer to sync.")
	}

	return di.checkObservedResourcesExist()
}

func (di *defaultImpl) registerCallbackAndStartInformer(stopCh <-chan struct{}) error {
	di.m.Lock()
	defer di.m.Unlock()
	if di.started {
		return errors.New("Watcher already started!")
	}
	di.started = true

	di.informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    di.addConfigMapEvent,
		UpdateFunc: di.updateConfigMapEvent,
	})

	// Start the shared informer factory (non-blocking)
	di.sif.Start(stopCh)
	return nil
}

func (di *defaultImpl) checkObservedResourcesExist() error {
	di.m.Lock()
	defer di.m.Unlock()
	// Check that all objects with Observers exist in our informers.
	for k := range di.observers {
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

	// Within our namespace, take the lock and see if there are any registered observers.
	di.m.Lock()
	defer di.m.Unlock()
	wl, ok := di.observers[configMap.Name]
	if !ok {
		return // No observers.
	}

	// Iterate over the observers and invoke their callbacks.
	for _, w := range wl {
		w(configMap)
	}
}

func (di *defaultImpl) updateConfigMapEvent(old, new interface{}) {
	di.addConfigMapEvent(new)
}
