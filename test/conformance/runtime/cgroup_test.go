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

package runtime

import (
	"fmt"
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"knative.dev/serving/test"

	. "knative.dev/serving/pkg/testing/v1"
)

const (
	cpuLimit    = 1     // CPU
	memoryLimit = 128   // MiB
	cpuRequest  = 0.125 // CPU
)

func toMilliValue(value float64) string {
	return fmt.Sprintf("%dm", int(value*1000))
}

// TestMustHaveCgroupConfigured verifies that the Linux cgroups are configured based on the specified
// resource limits and requests as delared by "MUST" in the runtime-contract.
func TestMustHaveCgroupConfigured(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	resources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(toMilliValue(cpuLimit)),
			corev1.ResourceMemory: resource.MustParse(strconv.Itoa(memoryLimit) + "Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse(toMilliValue(cpuRequest)),
		},
	}

	// Cgroup settings are based on the CPU and Memory Limits as well as CPU Reuqests
	// https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/
	//
	// It's important to make sure that the memory limit is divisible by common page
	// size (4k, 8k, 16k, 64k) as some environments apply rounding to the closest page
	// size multiple, see https://github.com/kubernetes/kubernetes/issues/82230.
	expectedCgroups := map[string]int{
		"/sys/fs/cgroup/memory/memory.limit_in_bytes": memoryLimit * 1024 * 1024, // 128 MiB
		"/sys/fs/cgroup/cpu/cpu.shares":               cpuRequest * 1024}         // CPURequests * 1024

	_, ri, err := fetchRuntimeInfo(t, clients, WithResourceRequirements(resources))
	if err != nil {
		t.Fatal("Error fetching runtime info:", err)
	}

	cgroups := ri.Host.Cgroups

	// These are used to check the ratio of 'period' to 'quota'. It needs to
	// be equal to the 'cpuLimit (limit = period / quota)
	var period, quota *int

	for _, cgroup := range cgroups {
		if cgroup.Error != "" {
			t.Error("Error getting cgroup information:", cgroup.Error)
			continue
		}

		// These two are special - just save their values and then continue
		if cgroup.Name == "/sys/fs/cgroup/cpu/cpu.cfs_period_us" {
			period = cgroup.Value
			continue
		}
		if cgroup.Name == "/sys/fs/cgroup/cpu/cpu.cfs_quota_us" {
			quota = cgroup.Value
			continue
		}

		if _, ok := expectedCgroups[cgroup.Name]; !ok {
			// Service returned a value we don't test
			t.Logf("%v cgroup returned, but not validated", cgroup.Name)
			continue
		}
		if got, want := *cgroup.Value, expectedCgroups[cgroup.Name]; got != want {
			t.Errorf("%s = %d, want: %d", cgroup.Name, *cgroup.Value, expectedCgroups[cgroup.Name])
		}
	}

	if period == nil {
		t.Error("Can't find the 'cpu.cfs_period_us' from cgroups")
	} else if quota == nil {
		t.Error("Can't find the 'cpu.cfs_quota_us' from cgroups")
	} else {
		percent := (100 * (*period)) / (*quota)
		if percent != cpuLimit*100 {
			t.Errorf("Percent (%v) is wrong should be %v. Period: %v Quota: %v",
				percent, cpuLimit*100, period, quota)
		}
	}

}

// TestShouldHaveCgroupReadOnly verifies that the Linux cgroups are mounted read-only within the
// container.
func TestShouldHaveCgroupReadOnly(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)
	_, ri, err := fetchRuntimeInfo(t, clients)
	if err != nil {
		t.Fatal("Error fetching runtime info:", err)
	}

	cgroups := ri.Host.Cgroups

	for _, cgroup := range cgroups {
		if cgroup.Error != "" {
			t.Error("Error getting cgroup information:", cgroup.Error)
			continue
		}
		if got, want := *cgroup.ReadOnly, true; got != want {
			t.Errorf("For cgroup %s cgroup.ReadOnly = %v, want: %v", cgroup.Name, *cgroup.ReadOnly, want)
		}
	}

}
