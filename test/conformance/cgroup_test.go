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
	"k8s.io/apimachinery/pkg/api/resource"

	corev1 "k8s.io/api/core/v1"
)

// TestMustCgroupConfigured verifies that the Linux cgroups are configured based on the specified
// resource limits and requests as delared by "MUST" in the runtime-contract.
func TestMustCgroupConfigured(t *testing.T) {
	logger := logging.GetContextLogger("TestMustCgroupConfigured")
	clients := setup(t)

	resources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("128M"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1"),
		},
	}

	// Cgroup settintgs are based on the CPU and Memory Limits as well as CPU Reuqests
	// https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/
	expectedCgroups := map[string]int{
		"/sys/fs/cgroup/memory/memory.limit_in_bytes": 128000000, // 128 MB
		"/sys/fs/cgroup/cpu/cpu.cfs_period_us":        100000,    // 100ms (100,000us) default
		"/sys/fs/cgroup/cpu/cpu.cfs_quota_us":         100000,    // 1000 millicore * 100
		"/sys/fs/cgroup/cpu/cpu.shares":               1024}      // CPURequests * 1024

	ri, err := fetchRuntimeInfo(clients, logger, &test.Options{ContainerResources: resources})
	if err != nil {
		t.Fatalf("unexpected error fetching runtime info: %v", err)
	}

	cgroups := ri.Host.Cgroups

	for _, cgroup := range cgroups {
		if cgroup.Error != "" {
			t.Errorf("unexpected error getting cgroup information: %v", cgroup.Error)
			continue
		}
		if expectedCgroups[cgroup.Name] == 0 {
			// Service returned a value we don't test
			logger.Infof("%v cgroup returned, but not validated", cgroup.Name)
			continue
		}
		if expectedCgroups[cgroup.Name] != *cgroup.Value {
			t.Errorf("%v = %v, want: %v", cgroup.Name, *cgroup.Value, expectedCgroups[cgroup.Name])
		}
	}
}

// TestShouldCgroupReadOnly verifies that the Linux cgroups are mounted read-only within the
// container.
func TestShouldCgroupReadOnly(t *testing.T) {
	logger := logging.GetContextLogger("TestShouldCgroupReadOnly")
	clients := setup(t)
	ri, err := fetchRuntimeInfo(clients, logger, &test.Options{})
	if err != nil {
		t.Fatalf("unexpected error fetching runtime info: %v", err)
	}

	cgroups := ri.Host.Cgroups

	for _, cgroup := range cgroups {
		if cgroup.Error != "" {
			t.Errorf("unexpected error getting cgroup information: %v", cgroup.Error)
			continue
		}
		if *cgroup.ReadOnly != true {
			t.Errorf("cgroup.ReadOnly = %v, want:true", *cgroup.ReadOnly)
		}
	}

}
