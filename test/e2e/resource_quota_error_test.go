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
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

func TestResourceQuotaError(t *testing.T) {
	t.Parallel()

	clients := test.Setup(t, test.Options{Namespace: "rq-test"})
	const (
		errorReason            = "RevisionFailed"
		progressDeadlineReason = "ProgressDeadlineExceeded"
		waitReason             = "ContainerCreating"
		errorMsgQuota          = "forbidden: exceeded quota"
		revisionReason         = "RevisionFailed"
	)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service", names.Image)
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("200m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("500m"),
		},
	}
	var (
		svc *v1.Service
		err error
	)
	if svc, err = v1test.CreateService(t,
		clients,
		names,
		rtesting.WithNamespace("rq-test"),
		rtesting.WithResourceRequirements(resources),
		rtesting.WithConfigAnnotations(map[string]string{serving.ProgressDeadlineAnnotationKey: "5s"}),
	); err != nil {
		t.Fatalf("Failed to create Service %s: %v", names.Service, err)
	}

	names.Config = serviceresourcenames.Configuration(svc)
	var cond *apis.Condition
	err = v1test.WaitForServiceState(clients.ServingClient, names.Service, func(r *v1.Service) (bool, error) {
		cond = r.Status.GetCondition(v1.ServiceConditionConfigurationsReady)
		if cond != nil && !cond.IsUnknown() {
			if cond.Reason == errorReason && cond.IsFalse() {
				return true, nil
			}
			t.Logf("Reason: %s ; Message: %s ; Status: %s", cond.Reason, cond.Message, cond.Status)
			return true, fmt.Errorf("the service %s was not marked with expected error condition (Reason=%q, Message=%q, Status=%q), but with (Reason=%q, Message=%q, Status=%q)",
				names.Config, errorReason, "", "False", cond.Reason, cond.Message, cond.Status)
		}
		return false, nil
	}, "ContainerUnscheduleable")

	if err != nil && !cond.IsUnknown() {
		t.Fatal("Failed to validate service state:", err)
	}

	revisionName, err := RevisionFromConfiguration(clients, names.Config)
	if err != nil {
		t.Fatalf("Failed to get revision from configuration %s: %v", names.Config, err)
	}

	if err := v1test.WaitForRevisionState(
		clients.ServingClient, revisionName, v1test.IsRevisionFailed, "RevisionFailed",
	); err != nil {
		t.Fatalf("The Revision %q did not fail: %v", revisionName, err)
	}

	t.Log("When the containers are not scheduled, the revision should have error status.")
	err = v1test.CheckRevisionState(clients.ServingClient, revisionName, func(r *v1.Revision) (bool, error) {
		cond := r.Status.GetCondition(v1.RevisionConditionReady)
		if cond != nil {
			if strings.Contains(cond.Message, errorMsgQuota) && cond.IsFalse() {
				return true, nil
			}
			// Can fail with either a progress deadline exceeded error
			if cond.Reason == progressDeadlineReason {
				return true, nil
			}
			// wait for the container creation
			if cond.Reason == waitReason {
				return false, nil
			}
			return true, fmt.Errorf("the revision %s was not marked with expected error condition (Reason=%q, Message=%q), but with (Reason=%q, Message=%q)",
				revisionName, revisionReason, errorMsgQuota, cond.Reason, cond.Message)
		}
		return false, nil
	})

	if err != nil {
		t.Fatal("Failed to validate revision state:", err)
	}
}
