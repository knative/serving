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
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	informers "k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// NewDefaultWatcher creates a new default configmap.Watcher instance.
//
// Deprecated: Use NewInformedWatcher
func NewDefaultWatcher(kc kubernetes.Interface, namespace string) *InformedWatcher {
	return NewInformedWatcher(kc, namespace)
}

// NewInformedWatcherFromFactory watches a Kubernetes namespace for configmap changes.
func NewInformedWatcherFromFactory(sif informers.SharedInformerFactory, namespace string) *InformedWatcher {
	return &InformedWatcher{
		sif:      sif,
		informer: sif.Core().V1().ConfigMaps(),
		ManualWatcher: ManualWatcher{
			Namespace: namespace,
		},
		defaults: make(map[string]*corev1.ConfigMap),
	}
}

// NewInformedWatcher watches a Kubernetes namespace for configmap changes.
func NewInformedWatcher(kc kubernetes.Interface, namespace string) *InformedWatcher {
	return NewInformedWatcherFromFactory(informers.NewSharedInformerFactoryWithOptions(
		kc,
		// This is the default resync period from controller-runtime.
		10*time.Hour,
		informers.WithNamespace(namespace),
	), namespace)
}

// InformedWatcher provides an informer-based implementation of Watcher.
type InformedWatcher struct {
	sif      informers.SharedInformerFactory
	informer corev1informers.ConfigMapInformer
	started  bool

	// defaults are the default ConfigMaps to use if the real ones do not exist or are deleted.
	defaults map[string]*corev1.ConfigMap

	// Embedding this struct allows us to reuse the logic
	// of registering and notifying observers. This simplifies the
	// InformedWatcher to just setting up the Kubernetes informer.
	ManualWatcher
}

// Asserts that InformedWatcher implements Watcher.
var _ Watcher = (*InformedWatcher)(nil)

// Asserts that InformedWatcher implements DefaultingWatcher.
var _ DefaultingWatcher = (*InformedWatcher)(nil)

// WatchWithDefault implements DefaultingWatcher.
func (i *InformedWatcher) WatchWithDefault(cm corev1.ConfigMap, o ...Observer) {
	i.defaults[cm.Name] = &cm

	i.m.Lock()
	started := i.started
	i.m.Unlock()
	if started {
		// TODO make both Watch and WatchWithDefault work after the InformedWatcher has started.
		// This likely entails changing this to `o(&cm)` and having Watch check started, if it has
		// started, then ensuring i.informer.Lister().ConfigMaps(i.Namespace).Get(cmName) exists and
		// calling this observer on it. It may require changing Watch and WatchWithDefault to return
		// an error.
		panic("cannot WatchWithDefault after the InformedWatcher has started")
	}

	i.Watch(cm.Name, o...)
}

// Start implements Watcher.
func (i *InformedWatcher) Start(stopCh <-chan struct{}) error {
	// Pretend that all the defaulted ConfigMaps were just created. This is done before we start
	// the informer to ensure that if a defaulted ConfigMap does exist, then the real value is
	// processed after the default one.
	for k := range i.observers {
		if def, ok := i.defaults[k]; ok {
			i.addConfigMapEvent(def)
		}
	}

	if err := i.registerCallbackAndStartInformer(stopCh); err != nil {
		return err
	}

	// Wait until it has been synced (WITHOUT holing the mutex, so callbacks happen)
	if ok := cache.WaitForCacheSync(stopCh, i.informer.Informer().HasSynced); !ok {
		return errors.New("error waiting for ConfigMap informer to sync")
	}

	return i.checkObservedResourcesExist()
}

func (i *InformedWatcher) registerCallbackAndStartInformer(stopCh <-chan struct{}) error {
	i.m.Lock()
	defer i.m.Unlock()
	if i.started {
		return errors.New("watcher already started")
	}
	i.started = true

	i.informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    i.addConfigMapEvent,
		UpdateFunc: i.updateConfigMapEvent,
		DeleteFunc: i.deleteConfigMapEvent,
	})

	// Start the shared informer factory (non-blocking).
	i.sif.Start(stopCh)
	return nil
}

func (i *InformedWatcher) checkObservedResourcesExist() error {
	i.m.Lock()
	defer i.m.Unlock()
	// Check that all objects with Observers exist in our informers.
	for k := range i.observers {
		_, err := i.informer.Lister().ConfigMaps(i.Namespace).Get(k)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				if _, ok := i.defaults[k]; ok {
					// It is defaulted, so it is OK that it doesn't exist.
					continue
				}
			}
			return err
		}
	}
	return nil
}

func (i *InformedWatcher) addConfigMapEvent(obj interface{}) {
	configMap := obj.(*corev1.ConfigMap)
	i.OnChange(configMap)
}

func (i *InformedWatcher) updateConfigMapEvent(old, new interface{}) {
	configMap := new.(*corev1.ConfigMap)
	i.OnChange(configMap)
}

func (i *InformedWatcher) deleteConfigMapEvent(obj interface{}) {
	configMap := obj.(*corev1.ConfigMap)
	if def, ok := i.defaults[configMap.Name]; ok {
		i.OnChange(def)
	}
	// If there is no default value, then don't do anything.
}
