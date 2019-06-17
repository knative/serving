/*
Copyright 2019 The Knative Authors

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

package logstream

import (
	"bufio"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/ptr"
	"github.com/knative/pkg/test"
	servingtest "github.com/knative/serving/test"
)

type kubelogs struct {
	Namespace string

	once sync.Once
	m    sync.RWMutex
	keys map[string]logger
}

type logger func(string, ...interface{})

var _ streamer = (*kubelogs)(nil)

// Init implements streamer
func (k *kubelogs) Init() {
	kc, err := test.NewKubeClient(test.Flags.Kubeconfig, test.Flags.Cluster)
	if err != nil {
		log.Fatalf("error loading client config: %v", err)
	}

	// List the pods in the given namespace.
	pl, err := kc.Kube.CoreV1().Pods(k.Namespace).List(metav1.ListOptions{})
	if err != nil {
		log.Fatalf("error listing pods: %v", err)
	}
	for _, pod := range pl.Items {
		// Grab data from all containers in the pods.  We need this in case
		// an envoy sidecar is injected for mesh installs.  This should be
		// equivalent to --all-containers.
		for _, container := range pod.Spec.Containers {
			go func(pod corev1.Pod, container corev1.Container) {
				options := &corev1.PodLogOptions{
					Container: container.Name,
					// Follow directs the api server to continuously stream logs back.
					Follow: true,
					// Only return new logs (this value is being used for "epsilon").
					SinceSeconds: ptr.Int64(1),
				}

				req := kc.Kube.CoreV1().Pods(k.Namespace).GetLogs(pod.Name, options)
				stream, err := req.Stream()
				if err != nil {
					log.Fatalf("error streaming pod logs: %v", err)
				}
				defer stream.Close()

				// Read this container's stream.
				for scanner := bufio.NewScanner(stream); scanner.Scan(); {
					k.handleLine(scanner.Text())
				}
			}(pod, container)
		}
	}
}

func (k *kubelogs) handleLine(l string) {
	// This holds the standard structure of our logs.
	var line struct {
		Level      string    `json:"level"`
		Timestamp  time.Time `json:"ts"`
		Controller string    `json:"knative.dev/controller"`
		Caller     string    `json:"caller"`
		Key        string    `json:"knative.dev/key"`
		Message    string    `json:"msg"`

		// TODO(mattmoor): Parse out more context.
	}
	if err := json.Unmarshal([]byte(l), &line); err != nil {
		// Ignore malformed lines.
		return
	}
	if line.Key == "" {
		return
	}

	k.m.RLock()
	defer k.m.RUnlock()

	for name, logf := range k.keys {
		// TODO(mattmoor): Do a slightly smarter match.
		if !strings.Contains(line.Key, name) {
			continue
		}
		// TODO(mattmoor): What information do we want to display?
		logf("[%s] %s", line.Controller, line.Message)
	}
}

// Start implements streamer
func (k *kubelogs) Start(t *testing.T) Canceler {
	k.once.Do(k.Init)

	k.m.Lock()
	defer k.m.Unlock()

	name := servingtest.ObjectPrefixForTest(t)
	// Register a key
	if k.keys == nil {
		k.keys = make(map[string]logger)
	}
	k.keys[name] = t.Logf

	// Return a function that unregisters that key.
	return func() {
		k.m.Lock()
		defer k.m.Unlock()

		delete(k.keys, name)
	}
}
