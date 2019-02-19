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

	"github.com/davecgh/go-spew/spew"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	corev1 "k8s.io/api/core/v1"
)

// TestShouldNotHaveHooks validates that we receive an error back when attempting to create a Service that
// specifies lifecycle hooks.
func TestShouldNotHaveHooks(t *testing.T) {
	t.Parallel()
	clients := setup(t)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   pizzaPlanet1,
	}

	hooks := []func(s *v1alpha1.Service){
		func(s *v1alpha1.Service) {
			lifecycleHandler := &corev1.ExecAction{
				Command: []string{"/bin/sh", "-c", "echo Hello from the post start handler > /usr/share/message"},
			}
			s.Spec.RunLatest.Configuration.RevisionTemplate.Spec.Container.Lifecycle = &corev1.Lifecycle{
				PostStart: &corev1.Handler{Exec: lifecycleHandler},
			}
		},

		func(s *v1alpha1.Service) {
			lifecycleHandler := &corev1.ExecAction{
				Command: []string{"/bin/sh", "-c", "echo Hello from the pre stop handler > /usr/share/message"},
			}
			s.Spec.RunLatest.Configuration.RevisionTemplate.Spec.Container.Lifecycle = &corev1.Lifecycle{
				PreStop: &corev1.Handler{Exec: lifecycleHandler},
			}
		},
	}

	for _, hook := range hooks {
		svc, err := test.CreateLatestService(t, clients, names, &test.Options{}, hook)
		if err == nil {
			t.Errorf("CreateLatestService = %v, want: error", spew.Sdump(svc))
		}
	}
}
