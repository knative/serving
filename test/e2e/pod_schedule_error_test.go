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

	"github.com/knative/pkg/test/logstream"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/service/resources/names"
	"github.com/knative/serving/test"
	v1a1test "github.com/knative/serving/test/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodScheduleError(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)
	const (
		errorReason    = "RevisionFailed"
		errorMsg       = "Insufficient cpu"
		revisionReason = "Unschedulable"
	)
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	t.Logf("Creating a new Service %s", names.Image)
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("50000m"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("50000m"),
		},
	}
	var (
		svc *v1alpha1.Service
		err error
	)
	if svc, err = v1a1test.CreateLatestService(t, clients, names, &v1a1test.Options{ContainerResources: resources}); err != nil {
		t.Fatalf("Failed to create Service %s: %v", names.Service, err)
	}

	names.Config = serviceresourcenames.Configuration(svc)

	err = v1a1test.WaitForServiceState(clients.ServingAlphaClient, names.Service, func(r *v1alpha1.Service) (bool, error) {
		cond := r.Status.GetCondition(v1alpha1.ConfigurationConditionReady)
		if cond != nil && !cond.IsUnknown() {
			if strings.Contains(cond.Message, errorMsg) && cond.IsFalse() {
				return true, nil
			}
			t.Logf("Reason: %s ; Message: %s ; Status: %s", cond.Reason, cond.Message, cond.Status)
			return true, fmt.Errorf("the service %s was not marked with expected error condition (Reason=\"%s\", Message=\"%s\", Status=\"%s\"), but with (Reason=\"%s\", Message=\"%s\", Status=\"%s\")",
				names.Config, errorReason, errorMsg, "False", cond.Reason, cond.Message, cond.Status)
		}
		return false, nil
	}, "ContainerUnscheduleable")

	if err != nil {
		t.Fatalf("Failed to validate service state: %s", err)
	}

	revisionName, err := revisionFromConfiguration(clients, names.Config)
	if err != nil {
		t.Fatalf("Failed to get revision from configuration %s: %v", names.Config, err)
	}

	t.Log("When the containers are not scheduled, the revision should have error status.")
	err = v1a1test.WaitForRevisionState(clients.ServingAlphaClient, revisionName, func(r *v1alpha1.Revision) (bool, error) {
		cond := r.Status.GetCondition(v1alpha1.RevisionConditionReady)
		if cond != nil {
			if cond.Reason == revisionReason && strings.Contains(cond.Message, errorMsg) {
				return true, nil
			}
			return true, fmt.Errorf("the revision %s was not marked with expected error condition (Reason=%q, Message=%q), but with (Reason=%q, Message=%q)",
				revisionName, revisionReason, errorMsg, cond.Reason, cond.Message)
		}
		return false, nil
	}, errorReason)

	if err != nil {
		t.Fatalf("Failed to validate revision state: %s", err)
	}
}

// Get revision name from configuration.
func revisionFromConfiguration(clients *test.Clients, configName string) (string, error) {
	config, err := clients.ServingAlphaClient.Configs.Get(configName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if config.Status.LatestCreatedRevisionName != "" {
		return config.Status.LatestCreatedRevisionName, nil
	}
	return "", fmt.Errorf("no valid revision name found in configuration %s", configName)
}
