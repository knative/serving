/*
Copyright 2018 Google LLC

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

package autoscaling

import (
	"testing"
	"time"

	. "github.com/elafros/elafros/pkg/controller/testing"
	"k8s.io/apimachinery/pkg/util/wait"
)

/* TODO tests:
- syncHandler returns error (in processNextWorkItem)
- invalid key in workqueue (in processNextWorkItem)
- object cannot be converted to key (in enqueueConfiguration)
- invalid key given to syncHandler
- resource doesn't exist in lister (from syncHandler)
*/
func TestNewRevisionCallsSyncHandler(t *testing.T) {
	rev := getTestRevision()
	// TODO(grantr): inserting the route at client creation is necessary
	// because ObjectTracker doesn't fire watches in the 1.9 client. When we
	// upgrade to 1.10 we can remove the config argument here and instead use the
	// Create() method.
	_, _, controller, _, _, stopCh := newRunningTestController(t, rev)
	defer close(stopCh)

	// Check that the controller has a scaler instance as a signal that
	// syncHandler ran
	err := wait.PollImmediate(time.Millisecond*100, time.Second*3, func() (bool, error) {
		if _, ok := controller.scalers[KeyOrDie(rev)]; ok {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		t.Errorf("No scaler found for %s: %v", KeyOrDie(rev), err)
	}
}
