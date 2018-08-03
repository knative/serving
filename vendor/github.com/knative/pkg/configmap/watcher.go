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
	"time"

	corev1 "k8s.io/api/core/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

// Observer is the signature of the callbacks that notify an observer of the latest
// state of a particular configuration.  An observer should not modify the provided
// ConfigMap, and should `.DeepCopy()` it for persistence (or otherwise process its
// contents).
type Observer func(*corev1.ConfigMap)

// Watcher defined the interface that a configmap implementation must implement.
type Watcher interface {
	// Watch is called to register a callback to be notified when a named ConfigMap changes.
	Watch(string, Observer)

	// Start is called to initiate the watches and provide a channel to signal when we should
	// stop watching.  When Start returns, all registered Observers will be called with the
	// initial state of the ConfigMaps they are watching.
	Start(<-chan struct{}) error
}

// NewDefaultWatcher creates a new default configmap.Watcher instance.
func NewDefaultWatcher(kc kubernetes.Interface, ns string) Watcher {
	sif := kubeinformers.NewFilteredSharedInformerFactory(
		kc, 5*time.Minute, ns, nil)

	return &defaultImpl{
		sif:      sif,
		informer: sif.Core().V1().ConfigMaps(),
		ns:       ns,
	}
}
