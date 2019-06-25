// +build postupgrade

/*
Copyright 2018 The Knative Authors

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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	revisionresourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	"knative.dev/serving/test/e2e"
)

func TestRunLatestServicePostUpgrade(t *testing.T) {
	t.Parallel()
	updateService(serviceName, t)
}

func TestRunLatestServicePostUpgradeFromScaleToZero(t *testing.T) {
	t.Parallel()

	clients, revisionName := updateService(scaleToZeroServiceName, t)
	revision, err := clients.ServingAlphaClient.Revisions.Get(revisionName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Getting revision %q: %v", revisionName, err)
	}

	if err := e2e.WaitForScaleToZero(t, revisionresourcenames.Deployment(revision), clients); err != nil {
		t.Fatalf("Could not scale to zero: %v", err)
	}
}
