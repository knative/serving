/*
Copyright 2018 Google LLC.

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

package configuration

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"

	hooks "github.com/knative/serving/pkg/controller/testing"
)

/* TODO tests:
- syncHandler returns error (in processNextWorkItem)
- invalid key in workqueue (in processNextWorkItem)
- object cannot be converted to key (in enqueueConfiguration)
- invalid key given to syncHandler
- resource doesn't exist in lister (from syncHandler)
*/

func TestNewConfigurationCallsSyncHandler(t *testing.T) {
	config := getTestConfiguration()
	// TODO(grantr): inserting the configuration at client creation is necessary
	// because ObjectTracker doesn't fire watches in the 1.9 client. When we
	// upgrade to 1.10 we can remove the config argument here and instead use the
	// Create() method.
	_, elaClient, _, _, _, stopCh := newRunningTestController(t, config)
	defer close(stopCh)

	h := hooks.NewHooks()

	// Check for revision created as a signal that syncHandler ran
	h.OnCreate(&elaClient.Fake, "revisions", func(obj runtime.Object) hooks.HookResult {
		rev := obj.(*v1alpha1.Revision)
		t.Logf("revision created: %q", rev.Name)

		return hooks.HookComplete
	})

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}
