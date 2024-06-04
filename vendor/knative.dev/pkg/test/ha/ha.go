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

package ha

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"knative.dev/pkg/test/logging"
)

func countingRFind(wr rune, wc int) func(rune) bool {
	cnt := 0
	return func(r rune) bool {
		if r == wr {
			cnt++
		}
		return cnt == wc
	}
}

func extractDeployment(pod string) string {
	if x := strings.LastIndexFunc(pod, countingRFind('-', 2)); x != -1 {
		return pod[:x]
	}
	return ""
}

// GetLeaders collects all of the leader pods from the specified deployment.
// GetLeaders will return duplicate pods by design.
func GetLeaders(ctx context.Context, t *testing.T, client kubernetes.Interface, deploymentName, namespace string) ([]string, error) {
	leases, err := client.CoordinationV1().Leases(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting leases for deployment %q: %w", deploymentName, err)
	}
	ret := make([]string, 0, len(leases.Items))
	for _, lease := range leases.Items {
		if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
			t.Logf("GetLeaders[%s] skipping lease %s as it has no holder", deploymentName, lease.Name)
			continue
		}
		pod := strings.SplitN(*lease.Spec.HolderIdentity, "_", 2)[0]

		// Deconstruct the pod name and look for the deployment.  This won't work for very long deployment names.
		if extractDeployment(pod) != deploymentName {
			continue
		}
		ret = append(ret, pod)
	}
	return ret, nil
}

// WaitForNewLeaders waits until the collection of current leaders consists of "n" leaders
// which do not include the specified prior leaders.
func WaitForNewLeaders(ctx context.Context, t *testing.T, client kubernetes.Interface, deploymentName, namespace string, previousLeaders sets.Set[string], n int) (sets.Set[string], error) {
	span := logging.GetEmitableSpan(ctx, "WaitForNewLeaders/"+deploymentName)
	defer span.End()

	var leaders sets.Set[string]
	err := wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		currLeaders, err := GetLeaders(ctx, t, client, deploymentName, namespace)
		if err != nil {
			return false, err
		}
		if len(currLeaders) < n {
			t.Logf("WaitForNewLeaders[%s] not enough leaders, got: %d, want: %d", deploymentName, len(currLeaders), n)
			return false, nil
		}
		l := sets.New[string](currLeaders...)
		if previousLeaders.HasAny(currLeaders...) {
			t.Logf("WaitForNewLeaders[%s] still see intersection: %v", deploymentName, previousLeaders.Intersection(l))
			return false, nil
		}
		leaders = l
		return true, nil
	})
	return leaders, err
}

// WaitForNewLeader waits until the holder of the given lease is different from the previousLeader.
//
// Deprecated: Use WaitForNewLeaders.
func WaitForNewLeader(ctx context.Context, client kubernetes.Interface, lease, namespace, previousLeader string) (string, error) {
	span := logging.GetEmitableSpan(ctx, "WaitForNewLeader/"+lease)
	defer span.End()
	var leader string
	err := wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
		lease, err := client.CoordinationV1().Leases(namespace).Get(ctx, lease, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("error getting lease %s: %w", lease, err)
		}
		leader = strings.Split(*lease.Spec.HolderIdentity, "_")[0]
		return leader != previousLeader, nil
	})
	return leader, err
}
