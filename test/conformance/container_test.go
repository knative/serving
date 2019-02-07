// +build e2e

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

package conformance

import (
	"testing"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	corev1 "k8s.io/api/core/v1"
)

func withLifecycle(s *v1alpha1.Service) {
	lifecycleHandler := &corev1.ExecAction{
		Command: []string{"/bin/sh", "-c", "echo Hello from the lifecycle handler > /usr/share/message"},
	}
	s.Spec.RunLatest.Configuration.RevisionTemplate.Spec.Container.Lifecycle = &corev1.Lifecycle{
		PostStart: &corev1.Handler{Exec: lifecycleHandler},
		PreStop:   &corev1.Handler{Exec: lifecycleHandler},
	}
}

// TestShouldNotHaveHooks validates that we receive an error back when attempting to create a Service that
// specifies lifecycle hooks.
func TestShouldNotHaveHooks(t *testing.T) {
	logger := logging.GetContextLogger(t.Name())
	clients := setup(t)
	names := test.ResourceNames{
		Service: test.AppendRandomString("test-should-not-have-hooks-", logger),
		Image:   pizzaPlanet1,
	}

	svc, err := test.CreateLatestService(logger, clients, names, &test.Options{}, withLifecycle)
	if err == nil {
		t.Errorf("CreateLatestService = %v, want: error", svc)
	}
}
