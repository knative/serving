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
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"k8s.io/client-go/kubernetes"
	"knative.dev/pkg/system"
	"knative.dev/pkg/test"
	"knative.dev/pkg/test/helpers"
	logstreamv2 "knative.dev/pkg/test/logstream/v2"
)

// Canceler is the type of a function returned when a logstream is started to be
// deferred so that the logstream can be stopped when the test is complete.
type (
	Canceler = logstreamv2.Canceler
	Callback = logstreamv2.Callback
)

type ti interface {
	Name() string
	Error(args ...interface{})
	Log(args ...interface{})
	Logf(fmt string, args ...interface{})
}

// Start begins streaming the logs from system components with a `key:` matching
// `test.ObjectNameForTest(t)` to `t.Log`. It returns a Canceler, which must
// be called before the test completes.
func Start(t ti) Canceler {
	// Do this lazily to make import ordering less important.
	once.Do(func() {
		if ns := os.Getenv(system.NamespaceEnvKey); ns != "" {
			var err error
			// handle case when ns contains a csv list
			namespaces := strings.Split(ns, ",")
			if sysStream, err = initStream(namespaces, true /*filterLines*/, nil /*podPrefixes*/); err != nil {
				t.Error("Error initializing logstream", "error", err)
			}
		} else {
			// Otherwise set up a null stream.
			sysStream = &null{}
		}
	})

	return sysStream.Start(t, t.Logf)
}

// StartForUserNamespace begins streaming the logs from custom namespaces.
// Filtering of log lines is disabled in this case so all lines will be printed
// throught the provided callback.
// It returns a Canceler which must be called before the test completes.
func StartForUserNamespace(t ti, callback Callback, namespace string, podPrefixes ...string) Canceler {
	userStream, err := initStream([]string{namespace}, false /*filterLines*/, podPrefixes)
	if err != nil {
		t.Error("Error initializing logstream", "error", err)
	}
	return userStream.Start(t, callback)
}

func initStream(namespaces []string, filterLines bool, podPrefixes []string) (streamer, error) {
	config, err := test.Flags.GetRESTConfig()
	if err != nil {
		return &null{}, fmt.Errorf("error loading client config: %w", err)
	}

	kc, err := kubernetes.NewForConfig(config)
	if err != nil {
		return &null{}, fmt.Errorf("error creating kubernetes client: %w", err)
	}

	return &shim{logstreamv2.FromNamespaces(context.Background(), kc, namespaces, filterLines, podPrefixes)}, nil
}

type streamer interface {
	Start(t ti, c Callback) Canceler
}

var (
	sysStream streamer
	once      sync.Once
)

type shim struct {
	logstreamv2.Source
}

func (s *shim) Start(t ti, callback Callback) Canceler {
	name := helpers.ObjectPrefixForTest(t)
	canceler, err := s.StartStream(name, callback)

	if err != nil {
		t.Error("Failed to start logstream", "error", err)
	}

	return canceler
}
