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

package e2e

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

func TestImagePullError(t *testing.T) {
	t.Parallel()

	clients := Setup(t)
	names := test.ResourceNames{
		Config: test.ObjectNameForTest(t),
		// TODO: Replace this when sha256 is broken.
		Image: "ubuntu@sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}

	test.EnsureTearDown(t, clients, &names)

	t.Logf("Creating a new Configuration  %s:%s", names.Config, names.Image)
	_, err := createLatestConfig(t, clients, names)
	if err != nil {
		t.Fatalf("Failed to create Config %s: %v", names.Config, err)
	}

	const wantCfgReason = "RevisionFailed"
	if err := v1test.WaitForConfigurationState(clients.ServingClient, names.Config, func(r *v1.Configuration) (bool, error) {
		cond := r.Status.GetCondition(v1.ConfigurationConditionReady)
		if cond != nil && !cond.IsUnknown() {
			if cond.IsFalse() {
				if cond.Reason == wantCfgReason && strings.Contains(cond.Message, "Back-off pulling image") {
					return true, nil
				}
			}
		}
		return false, nil
	}, "ContainerUnpullable"); err != nil {
		t.Fatal("Failed to validate configuration state:", err)
	}

	revisionName, err := RevisionFromConfiguration(clients, names.Config)
	if err != nil {
		t.Fatalf("Failed to get revision from configuration %s: %v", names.Config, err)
	}

	t.Log("When the images are not pulled, the revision should have error status.")
	wantRevReasons := sets.New("ImagePullBackOff", "ErrImagePull")
	if err := v1test.WaitForRevisionState(clients.ServingClient, revisionName, func(r *v1.Revision) (bool, error) {
		cond := r.Status.GetCondition(v1.RevisionConditionReady)
		if cond != nil {
			if wantRevReasons.Has(cond.Reason) {
				return true, nil
			}
		}
		return false, nil
	}, "RevisionWithErrorStatus"); err != nil {
		t.Fatal("Failed to validate revision state:", err)
	}
}

func createLatestConfig(t *testing.T, clients *test.Clients, names test.ResourceNames) (*v1.Configuration, error) {
	return v1test.CreateConfiguration(t, clients, names, func(c *v1.Configuration) {
		c.Spec = *v1test.ConfigurationSpec(names.Image)
	})
}
