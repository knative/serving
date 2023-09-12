//go:build e2e
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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"knative.dev/serving/test"
	"knative.dev/serving/test/types"

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

func isCgroupsV2(mounts []*types.Mount) (bool, error) {
	for _, mount := range mounts {
		if mount.Path == "/sys/fs/cgroup" {
			return mount.Type == "cgroup2", nil
		}
	}
	return false, fmt.Errorf("Failed to find cgroup mount on /sys/fs/cgroup")
}

// TestMustHaveCgroupConfigured verifies that the Linux cgroups are configured based on the specified
// resource limits and requests as delared by "MUST" in the runtime-contract.
func TestMustHaveCgroupConfigured(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	resources := createResources()

	_, ri, err := fetchRuntimeInfo(t, clients, WithResourceRequirements(resources))
	if err != nil {
		t.Fatal("Error fetching runtime info:", err)
	}

	// Cgroup settings are based on the CPU and Memory Limits as well as CPU Requests
	// https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/
	//
	// It's important to make sure that the memory limit is divisible by common page
	// size (4k, 8k, 16k, 64k) as some environments apply rounding to the closest page
	// size multiple, see https://github.com/kubernetes/kubernetes/issues/82230.
	var expectedCgroupsV1 = map[string]string{
		"/sys/fs/cgroup/memory/memory.limit_in_bytes": fmt.Sprintf("%d", resources.Limits.Memory().Value()&^4095), // floor() to 4K pages
		"/sys/fs/cgroup/cpu/cpu.shares":               fmt.Sprintf("%d", resources.Requests.Cpu().MilliValue()*1024/1000)}

	var expectedCgroupsV2 = map[string]string{
		"/sys/fs/cgroup/memory.max": fmt.Sprintf("%d", resources.Limits.Memory().Value()&^4095), // floor() to 4K pages
		// Convert cgroup v1 cpu.shares value to cgroup v2 cpu.weight
		// https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/2254-cgroup-v2#phase-1-convert-from-cgroups-v1-settings-to-v2
		"/sys/fs/cgroup/cpu.weight": fmt.Sprintf("%d", ((((resources.Requests.Cpu().MilliValue()*1024/1000)-2)*9999)/262142)+1),
	}

	cgroups := ri.Host.Cgroups
	cgroupV2, err := isCgroupsV2(ri.Host.Mounts)
	if err != nil {
		t.Fatal(err)
	}

	expectedCgroups := expectedCgroupsV1
	if cgroupV2 {
		t.Logf("using cgroupv2")
		expectedCgroups = expectedCgroupsV2
	}

	// These are used to check the ratio of 'quota' to 'period'. It needs to
	// be equal to the 'cpuLimit (limit = quota / period)
	var period, quota *string
	var maxV2, periodV2 string

	for _, cgroup := range cgroups {
		if cgroup.Error != "" {
			t.Error("Error getting cgroup information:", cgroup.Error)
			continue
		}

		// These are special - just save their values and then continue
		if cgroup.Name == "/sys/fs/cgroup/cpu/cpu.cfs_period_us" {
			period = cgroup.Value
			continue
		}
		if cgroup.Name == "/sys/fs/cgroup/cpu/cpu.cfs_quota_us" {
			quota = cgroup.Value
			continue
		}
		if cgroup.Name == "/sys/fs/cgroup/cpu.max" {
			// The format is like 'max 100000'.
			maxV2 = strings.Split(*cgroup.Value, " ")[0]
			periodV2 = strings.Split(*cgroup.Value, " ")[1]
		}

		if _, ok := expectedCgroups[cgroup.Name]; !ok {
			// Service returned a value we don't test
			t.Logf("%v cgroup returned, but not validated", cgroup.Name)
			continue
		}
		if got, want := *cgroup.Value, expectedCgroups[cgroup.Name]; got != want {
			t.Errorf("%s = %s, want: %s", cgroup.Name, *cgroup.Value, expectedCgroups[cgroup.Name])
		}
	}

	expectedCPULimit := int(resources.Limits.Cpu().MilliValue())
	if cgroupV2 {
		if maxV2 == "max" {
			t.Errorf("The cpu is unlimited but should be %d MilliCPUs", expectedCPULimit)
		}
		m, err := strconv.Atoi(maxV2)
		if err != nil {
			t.Error(err)
		}
		p, err := strconv.Atoi(periodV2)
		if err != nil {
			t.Error(err)
		}
		milliCPU := (1000 * m) / p
		if milliCPU != expectedCPULimit {
			t.Errorf("MilliCPU (%v) is wrong should be %v. Max: %v Period: %v", milliCPU, expectedCPULimit, m, p)
		}
	} else {
		if period == nil {
			t.Error("Can't find the 'cpu.cfs_period_us' from cgroups")
		} else if quota == nil {
			t.Error("Can't find the 'cpu.cfs_quota_us' from cgroups")
		} else {
			q, err := strconv.Atoi(*quota)
			if err != nil {
				t.Error(err)
			}
			p, err := strconv.Atoi(*period)
			if err != nil {
				t.Error(err)
			}
			// CustomCpuLimits of a core e.g. 125m means 12,5% of a single CPU, 2 or 2000m means 200% of a single CPU
			milliCPU := (1000 * q) / p
			if milliCPU != expectedCPULimit {
				t.Errorf("MilliCPU (%v) is wrong should be %v. Period: %v Quota: %v",
					milliCPU, expectedCPULimit, period, quota)
			}
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

func createResources() corev1.ResourceRequirements {
	resources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(toMilliValue(cpuLimit)),
			corev1.ResourceMemory: resource.MustParse(strconv.Itoa(memoryLimit) + "Mi")},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse(toMilliValue(cpuRequest)),
		},
	}

	if test.ServingFlags.CustomCPULimits != "" {
		resources.Limits[corev1.ResourceCPU] = resource.MustParse(test.ServingFlags.CustomCPULimits)
	}
	if test.ServingFlags.CustomMemoryLimits != "" {
		resources.Limits[corev1.ResourceMemory] = resource.MustParse(test.ServingFlags.CustomMemoryLimits)
	}
	if test.ServingFlags.CustomCPURequests != "" {
		resources.Requests[corev1.ResourceCPU] = resource.MustParse(test.ServingFlags.CustomCPURequests)
	}
	if test.ServingFlags.CustomMemoryRequests != "" {
		resources.Requests[corev1.ResourceMemory] = resource.MustParse(test.ServingFlags.CustomMemoryRequests)
	}
	return resources
}
