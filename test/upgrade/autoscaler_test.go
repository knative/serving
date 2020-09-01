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

	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/test/e2e"
)

const (
	containerConcurrency   = 6
	targetUtilization      = 0.7
	autoscaleTestImageName = "autoscale"
)

func TestAutoscaleSustaining(t *testing.T) {
	t.Parallel()
	// Create a named pipe and wait for the upgrade script to write to it
	// to signal that we should stop testing.
	createPipe(pipe, t)

	e2e.SetupSvc(t, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization, autoscaleTestImageName, e2e.ValidateEndpoint)

	stopCh := make(chan struct{})
	go func() {
		// e2e-upgrade-test.sh will close this pipe to signal the upgrade is
		// over, at which point we will finish the test and check the prober.
		ioutil.ReadFile(pipe)
		close(stopCh)
	}()
}
