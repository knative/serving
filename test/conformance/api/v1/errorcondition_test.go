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

package v1

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ptest "knative.dev/pkg/test"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"

	rtesting "knative.dev/serving/pkg/testing/v1"
)

const (
	containerMissing = "ContainerMissing"
)

// TestContainerErrorMsg is to validate the error condition defined at
// https://github.com/knative/docs/blob/master/docs/serving/spec/knative-api-specification-1.0.md#error-signalling
// for the container image missing scenario.
func TestContainerErrorMsg(t *testing.T) {
	t.Parallel()
	if strings.HasSuffix(strings.Split(ptest.Flags().DockerRepo, "/")[0], ".local") {
		t.Skip("Skipping for local docker repo")
	}
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.InvalidHelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	// Specify an invalid image path
	// A valid DockerRepo is still needed, otherwise will get UNAUTHORIZED instead of container missing error
	t.Log("Creating a new Service", names.Service)
	svc, err := v1test.CreateService(t, clients, names)
	if err != nil {
		t.Fatal("Failed to create Service:", err)
	}

	names.Config = serviceresourcenames.Configuration(svc)
	names.Route = serviceresourcenames.Route(svc)

	manifestUnknown := fmt.Sprint(http.StatusNotFound)

	// Wait for ServiceState becomes NotReady. It also waits for the creation of Configuration.
	t.Log("When the imagepath is invalid, the Configuration should have error status.")
	if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceAndChildrenFailed, "ServiceIsFailed"); err != nil {
		t.Fatalf("The Service %s was unexpected state: %v", names.Service, err)
	}

	// Checking for "Container image not present in repository" scenario defined in error condition spec
	err = v1test.CheckConfigurationState(clients.ServingClient, names.Config, func(r *v1.Configuration) (bool, error) {
		cond := r.Status.GetCondition(v1.ConfigurationConditionReady)
		if cond != nil && !cond.IsUnknown() {
			if strings.Contains(cond.Message, manifestUnknown) && cond.IsFalse() {
				return true, nil
			}
			t.Logf("Reason: %s; Message: %s; Status %s", cond.Reason, cond.Message, cond.Status)
			return true, fmt.Errorf("The configuration %s was not marked with expected error condition (Reason=%q, Message=%q, Status=%q), but with (Reason=%q, Message=%q, Status=%q)",
				names.Config, containerMissing, manifestUnknown, "False", cond.Reason, cond.Message, cond.Status)
		}
		t.Logf("Config %s Ready status = %v", names.Config, cond)
		return false, nil
	})

	if err != nil {
		t.Fatal("Failed to validate configuration state:", err)
	}

	revisionName, err := getRevisionFromConfiguration(clients, names.Config)
	if err != nil {
		t.Fatalf("Failed to get revision from configuration %s: %v", names.Config, err)
	}

	t.Log("When the imagepath is invalid, the revision should have error status.")
	if err = v1test.CheckRevisionState(clients.ServingClient, revisionName, func(r *v1.Revision) (bool, error) {
		cond := r.Status.GetCondition(v1.RevisionConditionReady)
		if cond != nil {
			if cond.Reason == containerMissing && strings.Contains(cond.Message, manifestUnknown) {
				return true, nil
			}
			return true, fmt.Errorf("The revision %s was not marked with expected error condition (Reason=%q, Message=%q), but with (Reason=%q, Message=%q)",
				revisionName, containerMissing, manifestUnknown, cond.Reason, cond.Message)
		}
		t.Logf("Revision %s Ready state = %#v", revisionName, cond)
		return false, nil
	}); err != nil {
		t.Fatal("Failed to validate revision state:", err)
	}

	t.Log("Checking to ensure Route is in desired state")
	err = v1test.CheckRouteState(clients.ServingClient, names.Route, v1test.IsRouteFailed)
	if err != nil {
		t.Fatalf("the Route %s was not desired state: %v", names.Route, err)
	}
}

// TestContainerExitingMsg is to validate the error condition defined at
// https://github.com/knative/serving/blob/master/docs/spec/errors.md
// for the container crashing scenario.
func TestContainerExitingMsg(t *testing.T) {
	t.Parallel()
	const (
		// The given image will always exit with an exit code of 5
		exitCodeReason = "ExitCode5"
		// ... and will print "Crashed..." before it exits
		errorLog = "Crashed..."
	)

	tests := []struct {
		Name           string
		ReadinessProbe *corev1.Probe
	}{{
		Name: "http",
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{},
			},
		},
	}, {
		Name: "tcp",
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				TCPSocket: &corev1.TCPSocketAction{},
			},
		},
	}}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			clients := test.Setup(t)

			names := test.ResourceNames{
				Config: test.ObjectNameForTest(t),
				Image:  test.Failing,
			}

			test.EnsureTearDown(t, clients, &names)

			t.Log("Creating a new Configuration", names.Config)

			if _, err := v1test.CreateConfiguration(t, clients, names, rtesting.WithConfigReadinessProbe(tt.ReadinessProbe)); err != nil {
				t.Fatalf("Failed to create configuration %s: %v", names.Config, err)
			}

			t.Log("When the containers keep crashing, the Configuration should have error status.")
			if err := v1test.WaitForConfigurationState(clients.ServingClient, names.Config, func(r *v1.Configuration) (bool, error) {
				names.Revision = r.Status.LatestCreatedRevisionName
				cond := r.Status.GetCondition(v1.ConfigurationConditionReady)
				if cond != nil && !cond.IsUnknown() {
					return true, nil
				}
				t.Logf("Configuration %s Ready = %#v", names.Config, cond)
				return false, nil
			}, "ConfigContainersCrashing"); err != nil {
				t.Fatal("Failed to validate configuration state:", err)
			}

			t.Log("When the containers keep crashing, the revision should have error status.")
			err := v1test.CheckRevisionState(clients.ServingClient, names.Revision, func(r *v1.Revision) (bool, error) {
				// We may not be the only condition surfacing this failure status, so instead of requiring the Ready
				// condition to pick our message to bubble up, just check that one of the failing sub conditions has
				for _, cond := range r.Status.Conditions {
					if cond.Reason == exitCodeReason && strings.Contains(cond.Message, errorLog) {
						return true, nil
					}
				}
				return true, fmt.Errorf("the revision %s was not marked with expected error condition (Reason=%s, Message=%q), but with %#v",
					names.Revision, exitCodeReason, errorLog, r.Status.Conditions)
			})
			if err != nil {
				t.Fatal("Failed to validate revision state:", err)
			}
		})
	}
}

// Get revision name from configuration.
func getRevisionFromConfiguration(clients *test.Clients, configName string) (string, error) {
	config, err := clients.ServingClient.Configs.Get(context.Background(), configName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if config.Status.LatestCreatedRevisionName != "" {
		return config.Status.LatestCreatedRevisionName, nil
	}
	return "", fmt.Errorf("No valid revision name found in configuration %s", configName)
}
