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

package initscale

import (
	"testing"

	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
)

// TestInitScaleZero tests setting of annotation initialScale to 0 on
// the revision level. This test runs after the cluster wide flag allow-zero-initial-scale
// is set to true.
func TestInitScaleZero(t *testing.T) {
	t.Parallel()

	clients := e2e.Setup(t)
	names := test.ResourceNames{
		Config: test.ObjectNameForTest(t),
		Image:  "helloworld",
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Configuration with initial scale zero and verifying that no pods are created")
	e2e.CreateAndVerifyInitialScaleConfiguration(t, clients, names, 0)
}
