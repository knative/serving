// +build e2e

/*
Copyright 2020 The Knative Authors

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

package gc

import (
	"testing"

	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

// TODO(whaught): This tests that the labeler applies the new label, but we need to update the GC config
// and assert deletion of old revisions.
func TestRevisionGC(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := e2e.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	revision := resources.Revision
	if val, ok := revision.Labels[serving.RoutingStateLabelKey]; ok {
		if val != "active" {
			t.Fatalf(`Got revision label %s=%q, want="active"`, serving.RoutingStateLabelKey, val)
		}
	} else {
		t.Fatalf("Failed to get revision label %q", serving.RoutingStateLabelKey)
	}
}
