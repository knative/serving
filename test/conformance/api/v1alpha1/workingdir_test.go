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

package v1alpha1

import (
	"testing"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	v1a1test "github.com/knative/serving/test/v1alpha1"
)

func TestWorkingDirService(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.WorkingDir,
	}

	// Clean up on test failure or interrupt
	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	const wd = "/foo/bar/baz"

	// Setup initial Service
	_, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, &v1a1test.Options{},
		func(svc *v1alpha1.Service) {
			c := &svc.Spec.Template.Spec.Containers[0]
			c.WorkingDir = wd
		})
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	if err = validateRunLatestDataPlane(t, clients, names, wd); err != nil {
		t.Error(err)
	}
}
