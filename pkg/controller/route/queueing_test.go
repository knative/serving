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

package route

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/knative/serving/pkg/apis/ela/v1alpha1"
	corev1 "k8s.io/api/core/v1"

	. "github.com/knative/serving/pkg/controller/testing"
)

/* TODO tests:
- syncHandler returns error (in processNextWorkItem)
- invalid key in workqueue (in processNextWorkItem)
- object cannot be converted to key (in enqueueConfiguration)
- invalid key given to syncHandler
- resource doesn't exist in lister (from syncHandler)
*/

func TestNewRouteCallsSyncHandler(t *testing.T) {
	// A standalone revision
	rev := getTestRevision("test-rev")
	// A route targeting the revision
	route := getTestRouteWithTrafficTargets(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				RevisionName: "test-rev",
				Percent:      100,
			},
		},
	)
	// TODO(grantr): inserting the route at client creation is necessary
	// because ObjectTracker doesn't fire watches in the 1.9 client. When we
	// upgrade to 1.10 we can remove the config argument here and instead use the
	// Create() method.
	kubeClient, _, _, _, _, stopCh := newRunningTestController(t, rev, route)
	defer close(stopCh)

	h := NewHooks()

	// Check for service created as a signal that syncHandler ran
	h.OnCreate(&kubeClient.Fake, "services", func(obj runtime.Object) HookResult {
		service := obj.(*corev1.Service)
		t.Logf("service created: %q", service.Name)

		return HookComplete
	})

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}
