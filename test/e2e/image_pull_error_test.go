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
	"fmt"
	"strings"
	"testing"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/service/resources/names"
	v1alpha1testing "github.com/knative/serving/pkg/reconciler/testing"
	"github.com/knative/serving/test"
)

func TestImagePullError(t *testing.T) {
	clients := Setup(t)
	const (
		errorReason   = "RevisionFailed"
		backoffMsg    = "Back-off pulling image"
		backoffReason = "ImagePullBackOff"
		daemonMsg     = "Error response from daemon: manifest for"
		daemonReason  = "ErrImagePull"
	)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		// TODO: Replace this when sha256 is broken.
		Image: "ubuntu@sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}

	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	t.Logf("Creating a new Service %s", names.Image)
	var (
		svc *v1alpha1.Service
		err error
	)
	if svc, err = createLatestService(t, clients, names); err != nil {
		t.Fatalf("Failed to create Service %s: %v", names.Service, err)
	}

	names.Config = serviceresourcenames.Configuration(svc)

	err = test.WaitForServiceState(clients.ServingClient, names.Service, func(r *v1alpha1.Service) (bool, error) {
		cond := r.Status.GetCondition(v1alpha1.ConfigurationConditionReady)
		if cond != nil && !cond.IsUnknown() {
			if cond.IsFalse() {
				if strings.Contains(cond.Message, backoffMsg) ||
					strings.Contains(cond.Message, daemonMsg) {
					return true, nil
				}
			}
			t.Logf("Reason: %s ; Message: %s ; Status: %s", cond.Reason, cond.Message, cond.Status)
			return true, fmt.Errorf("the service %s was not marked with expected error condition, but with (Reason=\"%s\", Message=\"%s\", Status=\"%s\")",
				names.Service, cond.Reason, cond.Message, cond.Status)
		}
		return false, nil
	}, "ContainerUnpullable")

	if err != nil {
		t.Fatalf("Failed to validate service state: %s", err)
	}

	revisionName, err := revisionFromConfiguration(clients, names.Config)
	if err != nil {
		t.Fatalf("Failed to get revision from configuration %s: %v", names.Config, err)
	}

	t.Log("When the images are not pulled, the revision should have error status.")
	err = test.WaitForRevisionState(clients.ServingClient, revisionName, func(r *v1alpha1.Revision) (bool, error) {
		cond := r.Status.GetCondition(v1alpha1.RevisionConditionReady)
		if cond != nil {
			if (cond.Reason == backoffReason && strings.Contains(cond.Message, backoffMsg)) ||
				(cond.Reason == daemonReason && strings.Contains(cond.Message, daemonMsg)) {
				return true, nil
			}
			return true, fmt.Errorf("the revision %s was not marked with expected error condition, but with (Reason=%q, Message=%q)",
				revisionName, cond.Reason, cond.Message)
		}
		return false, nil
	}, errorReason)

	if err != nil {
		t.Fatalf("Failed to validate revision state: %s", err)
	}
}

// Wrote our own thing so that we can pass in an image by digest.
// knative/pkg/test.ImagePath currently assumes there's a tag, which fails to parse.
func createLatestService(t *testing.T, clients *test.Clients, names test.ResourceNames) (*v1alpha1.Service, error) {
	opt := v1alpha1testing.WithInlineConfigSpec(*test.ConfigurationSpec(names.Image, &test.Options{}))
	service := v1alpha1testing.ServiceWithoutNamespace(names.Service, opt)
	test.LogResourceObject(t, test.ResourceObjects{Service: service})
	return clients.ServingClient.Services.Create(service)
}
