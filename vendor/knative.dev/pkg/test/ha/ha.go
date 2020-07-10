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
	"knative.dev/pkg/test"
	"knative.dev/pkg/test/logging"
)

// GetLeaders collects all of the leader pods from the specified deployment.
func GetLeaders(t *testing.T, client *test.KubeClient, deploymentName, namespace string) ([]string, error) {
	leases, err := client.Kube.CoordinationV1().Leases(namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting leases for deployment %q: %w", deploymentName, err)
	}
	var pods []string
	for _, lease := range leases.Items {
		if lease.Spec.HolderIdentity == nil {
			continue
		}
		pod := strings.SplitN(*lease.Spec.HolderIdentity, "_", 2)[0]

		// Deconstruct the pod name and look for the deployment.  This won't work for very long deployment names.
		parts := strings.Split(pod, "-")
		if len(parts) < 3 {
			continue
		}
		if strings.Join(parts[:len(parts)-2], "-") != deploymentName {
			continue
		}
		pods = append(pods, pod)
	}
	return pods, nil
}

// WaitForNewLeaders waits until the collection of current leaders consists of "n" leaders
// which do not include the specified prior leaders.
func WaitForNewLeaders(t *testing.T, client *test.KubeClient, deploymentName, namespace string, previousLeaders sets.String, n int) (sets.String, error) {
	span := logging.GetEmitableSpan(context.Background(), "WaitForNewLeaders/"+deploymentName)
	defer span.End()

	var leaders sets.String
	err := wait.PollImmediate(time.Second, time.Minute, func() (bool, error) {
		currentLeaders, err := GetLeaders(t, client, deploymentName, namespace)
		if err != nil {
			return false, err
		}
		if len(currentLeaders) < n {
			t.Logf("WaitForNewLeaders[%s] not enough leaders, got: %d, want: %d", deploymentName, len(currentLeaders), n)
			return false, nil
		}
		if previousLeaders.HasAny(currentLeaders...) {
			t.Logf("WaitForNewLeaders[%s] still see intersection: %v", deploymentName, previousLeaders.Intersection(sets.NewString(currentLeaders...)))
			return false, nil
		}
		leaders = sets.NewString(currentLeaders...)
		return true, nil
	})
	return leaders, err
}

// DEPRECATED WaitForNewLeader waits until the holder of the given lease is different from the previousLeader.
func WaitForNewLeader(client *test.KubeClient, lease, namespace, previousLeader string) (string, error) {
	span := logging.GetEmitableSpan(context.Background(), "WaitForNewLeader/"+lease)
	defer span.End()
	var leader string
	err := wait.PollImmediate(time.Second, time.Minute, func() (bool, error) {
		lease, err := client.Kube.CoordinationV1().Leases(namespace).Get(lease, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("error getting lease %s: %w", lease, err)
		}
		leader = strings.Split(*lease.Spec.HolderIdentity, "_")[0]
		return leader != previousLeader, nil
	})
	return leader, err
}
