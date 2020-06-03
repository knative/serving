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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/test"
	"knative.dev/pkg/test/logging"
)

// WaitForNewLeader waits until the holder of the given lease is different from the previousLeader.
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
