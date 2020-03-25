/*
Copyright 2020 The Knative Authors

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

// Simple abstraction for storing state on a k8s ConfigMap. Very very simple
// and uses a single entry in the ConfigMap.data for storing serialized
// JSON of the generic data that Load/Save uses. Handy for things like sources
// that need to persist some state (checkpointing for example).
package kvstore

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"knative.dev/pkg/logging"
)

type configMapKVStore struct {
	cmClient  v1.ConfigMapInterface
	name      string
	namespace string
	data      map[string]string
}

var (
	_ Interface = (*configMapKVStore)(nil)
)

func NewConfigMapKVStore(ctx context.Context, name string, namespace string, clientset v1.CoreV1Interface) Interface {

	return &configMapKVStore{name: name, namespace: namespace, cmClient: clientset.ConfigMaps(namespace)}
}

// Init initializes configMapKVStore either by loading or creating an empty one.
func (cs *configMapKVStore) Init(ctx context.Context) error {
	l := logging.FromContext(ctx)
	l.Info("Initializing configMapKVStore...")

	err := cs.Load(ctx)
	if apierrors.IsNotFound(err) {
		l.Info("No config found, creating empty")
		return cs.createConfigMap()
	}
	return err
}

// Load fetches the ConfigMap from k8s and unmarshals the data found
// in the configdatakey type as specified by value.
func (cs *configMapKVStore) Load(ctx context.Context) error {
	cm, err := cs.cmClient.Get(cs.name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	cs.data = cm.Data
	return nil
}

// Save takes the value given in, and marshals it into a string
// and saves it into the k8s ConfigMap under the configdatakey.
func (cs *configMapKVStore) Save(ctx context.Context) error {
	cm, err := cs.cmClient.Get(cs.name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	cm.Data = cs.data
	_, err = cs.cmClient.Update(cm)
	return err
}

// Get retrieves and unmarshals the value from the map.
func (cs *configMapKVStore) Get(ctx context.Context, key string, value interface{}) error {
	v, ok := cs.data[key]
	if !ok {
		return fmt.Errorf("key %s does not exist", key)
	}
	err := json.Unmarshal([]byte(v), value)
	if err != nil {
		return fmt.Errorf("Failed to Unmarshal %q: %v", v, err)
	}
	return nil
}

// Set marshals and sets the value given under specified key.
func (cs *configMapKVStore) Set(ctx context.Context, key string, value interface{}) error {
	bytes, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("Failed to Marshal: %v", err)
	}
	cs.data[key] = string(bytes)
	return nil
}

func (cs *configMapKVStore) createConfigMap() error {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cs.name,
			Namespace: cs.namespace,
		},
	}
	_, err := cs.cmClient.Create(cm)
	return err
}
