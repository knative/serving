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
	"testing"

	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

func TestRollbackBYOName(t *testing.T) {
	t.Parallel()

	clients := Setup(t)

	serviceName := test.ObjectNameForTest(t)
	byoNameOld := serviceName + "-byo-foo"
	byoNameNew := serviceName + "-byo-foo-new"
	names := test.ResourceNames{
		Service: serviceName,
		Images:  []string{"helloworld"},
	}

	test.EnsureTearDown(t, clients, &names)

	withTrafficSpecOld := rtesting.WithRouteSpec(v1.RouteSpec{
		Traffic: []v1.TrafficTarget{{
			RevisionName: byoNameOld,
			Percent:      ptr.Int64(100),
		}},
	})
	withTrafficSpecNew := rtesting.WithRouteSpec(v1.RouteSpec{
		Traffic: []v1.TrafficTarget{{
			RevisionName: byoNameNew,
			Percent:      ptr.Int64(100),
		}},
	})

	t.Logf("Creating a new Service with byo config name %q.", byoNameOld)
	resources, err := v1test.CreateServiceReady(t, clients, &names,
		withTrafficSpecOld,
		func(svc *v1.Service) {
			svc.Spec.ConfigurationSpec.Template.ObjectMeta.Name = byoNameOld
		})
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}
	originalServiceSpec := resources.Service.Spec
	revisionName := resources.Revision.ObjectMeta.Name
	if revisionName != byoNameOld {
		t.Fatalf("Expect configuration name in revision label %q but got %q ", byoNameOld, revisionName)
	}

	// Update service to use a new byo name
	t.Logf("Updating the Service to a new revision with a new byo name %q.", byoNameNew)
	svc, err := v1test.PatchService(t, clients, resources.Service, func(s *v1.Service) {
		s.Spec.Template.Name = byoNameNew
		withTrafficSpecNew(s)
	})
	resources.Service = svc
	if err != nil {
		t.Fatalf("Patch update for Service (new byo name %q) failed: %v", byoNameNew, err)
	}

	t.Log("Since the Service was updated a new Revision will be created and the Service will be updated")
	newRevision, err := v1test.WaitForServiceLatestRevision(clients, names)
	if err != nil {
		t.Fatalf("Service %s was not updated with the Revision for new byo name %s: %v", names.Service, byoNameNew, err)
	}
	if newRevision != byoNameNew {
		t.Fatalf("Expect configuration name in revision label %q but got %q ", byoNameNew, newRevision)
	}

	// Now, rollback to the first RevisionSpec
	svc, err = v1test.PatchService(t, clients, resources.Service, func(s *v1.Service) {
		s.Spec = originalServiceSpec
	})
	resources.Service = svc
	if err != nil {
		t.Fatalf("Patch update for Service (rollback to byo name %q) failed: %v", byoNameOld, err)
	}

	t.Logf("We are rolling back to the previous revision (byoNameOld %q).", byoNameOld)
	// Wait for the route to become ready, and check that the traffic split between the byoNameOld
	// and byoNameNew is 100 and 0, respectively
	err = v1test.WaitForServiceState(clients.ServingClient, names.Service, func(s *v1.Service) (bool, error) {
		for _, tr := range s.Status.Traffic {
			if tr.RevisionName != byoNameOld {
				return false, nil
			}
			if tr.Percent == nil || *tr.Percent != 100 {
				return false, nil
			}
		}
		return true, nil
	}, "ServiceRollbackRevision")
	if err != nil {
		t.Fatalf("Service %s was not rolled back with byo name %s: %v", names.Service, byoNameOld, err)
	}

	// Verify that the latest ready revision and latest created revision are both byoNameNew,
	// which means no new revision is created in the rollback
	err = v1test.WaitForServiceState(clients.ServingClient, names.Service, func(s *v1.Service) (bool, error) {
		return (s.Status.LatestReadyRevisionName == byoNameOld && s.Status.LatestCreatedRevisionName == byoNameOld), nil
	}, "ServiceNoNewRevisionCreated")
	if err != nil {
		t.Fatalf("Service %s was not rolled back with byo name %s: %v", names.Service, byoNameOld, err)
	}
}
