// +build probe

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

package upgrade

import (
	"io/ioutil"
	"testing"
	"time"

	"knative.dev/serving/pkg/apis/autoscaling"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test/e2e"
)

const (
	containerConcurrency = 6
	targetUtilization    = 0.7

	autoscalingPipe    = "/tmp/autoscaling-signal"
	autoscalingTBCPipe = "/tmp/autoscaling-tbc-signal"
)

// This test similar to TestAutoscaleSustaining in test/e2e/autoscale_test.go. It asserts
// the pods number is sustained during the whole cluster upgrade/downgrade process.
func TestAutoscaleSustaining(t *testing.T) {
	t.Parallel()
	// Create a named pipe and wait for the upgrade script to write to it
	// to signal that we should stop testing.
	createPipe(t, autoscalingPipe)

	ctx := e2e.SetupSvc(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "0", // Not let Activator in the path.
		}))

	stopCh := make(chan time.Time)
	go func() {
		// e2e-upgrade-test.sh will close this pipe to signal the upgrade is
		// over, at which point we will finish the test.
		ioutil.ReadFile(autoscalingPipe)
		close(stopCh)
	}()

	e2e.AssertAutoscaleUpToNumPods(ctx, 1, 10, stopCh, false /* quick */)
}

func TestAutoscaleSustainingWithTBC(t *testing.T) {
	t.Parallel()
	// Create a named pipe and wait for the upgrade script to write to it
	// to signal that we should stop testing.
	createPipe(t, autoscalingTBCPipe)

	ctx := e2e.SetupSvc(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization,
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1", // Put Activator always in the path.
		}))

	stopCh := make(chan time.Time)
	go func() {
		// e2e-upgrade-test.sh will close this pipe to signal the upgrade is
		// over, at which point we will finish the test.
		ioutil.ReadFile(autoscalingTBCPipe)
		close(stopCh)
	}()

	e2e.AssertAutoscaleUpToNumPods(ctx, 1, 10, stopCh, false /* quick */)
}
