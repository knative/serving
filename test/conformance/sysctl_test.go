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
	"github.com/knative/serving/test"
)

// TestShouldHaveSysctlReadOnly verifies that the /proc/sys filesystem mounted within the container
// is read-only.
func TestShouldHaveSysctlReadOnly(t *testing.T) {
	logger := logging.GetContextLogger(t.Name())
	clients := setup(t)
	ri, err := fetchRuntimeInfo(clients, logger, &test.Options{})
	if err != nil {
		t.Fatalf("Error fetching runtime info: %v", err)
	}

	mounts := ri.Host.Mounts

	for _, mount := range mounts {
		if mount.Error != "" {
			t.Fatalf("Error getting mount information: %v", mount.Error)
		}
		if mount.Path == "/proc/sys" {
			if got, want := mount.Type, "proc"; got != want {
				t.Errorf("mount.Type = %v, wanted: %v", mount.Type, want)
			}
			if got, want := mount.Device, "proc"; got != want {
				t.Errorf("mount.Device = %v, wanted: %v", mount.Device, want)
			}
			for _, option := range mount.Options {
				// Read Only must be present in options list
				if option == "ro" {
					return
				}
			}
			t.Fatalf("mount.Options = %v, wanted: ro", mount.Options)
		}
	}
	t.Fatalf("/proc/sys missing from mount list")
}
