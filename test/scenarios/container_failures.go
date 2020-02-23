// +build e2e

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

package scenarios

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logging"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// ContainerError executes a test flow where the container returns an Error when it attempts to start.
// The caller can inject its testing at two points:
// * falseConfigurationValidator: when the Configuration state becomes false, this function is called
// * revisionValidator: when checking the Revision state
// In both cases, the Condition has already been validated by ValidateCondition and relevant fields are
// already included in the TLogger.
func ContainerError(t *logging.TLogger, falseConfigurationValidator func(*logging.TLogger, *apis.Condition) (bool, error), revisionValidator func(*logging.TLogger, *apis.Condition) (bool, error)) {
	if strings.HasSuffix(strings.Split(pkgTest.Flags.DockerRepo, "/")[0], ".local") {
		t.V(0).Info("Skipping for local docker repo")
		t.SkipNow()
	}
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.InvalidHelloWorld,
	}

	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	// Specify an invalid image path
	// A valid DockerRepo is still needed, otherwise will get UNAUTHORIZED instead of container missing error
	t.V(2).Info("Creating a new Service", "service", names.Service)
	svc, err := v1test.CreateService(t, clients, names, rtesting.WithRevisionTimeoutSeconds(2))
	t.FatalIfErr(err, "Failed to create Service")

	names.Config = serviceresourcenames.Configuration(svc)
	names.Route = serviceresourcenames.Route(svc)

	t.Run("API", func(ts *logging.TLogger) {
		ts.V(1).Info("When the imagepath is invalid, the Configuration should have error status.")
		ts.V(8).Info("Wait for ServiceState becomes NotReady. It also waits for the creation of Configuration.")
		err = v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceNotReady, "ServiceIsNotReady")
		ts.FatalIfErr(err, "The Service was unexpected state",
			"service", names.Service)

		ts.V(8).Info("Checking for 'Container image not present in repository' scenario defined in error condition spec.")
		err = v1test.WaitForConfigurationState(clients.ServingClient, names.Config, func(r *v1.Configuration) (bool, error) {
			cond := r.Status.GetCondition(v1.ConfigurationConditionReady)
			ts.WithValues("configuration", names.Config, "condition", cond)
			v1test.ValidateCondition(ts, cond)
			if cond != nil && !cond.IsUnknown() {
				if cond.IsFalse() {
					return falseConfigurationValidator(ts, cond)
				} else {
					ts.Fatal("Configuration should not have become ready")
				}
			}
			return false, nil
		}, "ContainerImageNotPresent")

		ts.FatalIfErr(err, "Failed to validate configuration state")

		revisionName, err := getRevisionFromConfiguration(clients, names.Config)
		ts.FatalIfErr(err, "Failed to get revision from configuration", "configuration", names.Config)

		ts.V(1).Info("When the imagepath is invalid, the revision should have error status.")
		err = v1test.WaitForRevisionState(clients.ServingClient, revisionName, func(r *v1.Revision) (bool, error) {
			cond := r.Status.GetCondition(v1.RevisionConditionReady)
			ts := ts.WithValues("revision", revisionName, "condition", cond)
			v1test.ValidateCondition(ts, cond)
			if cond != nil {
				return revisionValidator(ts, cond)
			}
			return false, nil
		}, "ImagePathInvalid")

		ts.FatalIfErr(err, "Failed to validate revision state")

		ts.V(1).Info("Checking to ensure Route is in desired state")
		err = v1test.CheckRouteState(clients.ServingClient, names.Route, v1test.IsRouteNotReady)
		ts.FatalIfErr(err, "The Route was not desired state", "route", names.Route)
	})
}

// ContainerError executes a test flow where the container exists shortly after successfully starting.
// The caller can inject its testing at two points:
// * falseConfigurationValidator: when the Configuration state becomes false, this function is called
// * revisionValidator: when checking the Revision state
// In both cases, the Condition has already been validated by ValidateCondition and relevant fields are
// already included in the TLogger.
func ContainerExiting(t *logging.TLogger, falseConfigurationValidator func(*logging.TLogger, *apis.Condition) (bool, error), revisionValidator func(*logging.TLogger, *apis.Condition) (bool, error)) {
	t.Parallel()
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
		t.Run(tt.Name, func(t *logging.TLogger) {
			t.Parallel()
			clients := test.Setup(t)

			names := test.ResourceNames{
				Config: test.ObjectNameForTest(t),
				Image:  test.Failing,
			}

			defer test.TearDown(clients, names)
			test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

			t.Run("API", func(ts *logging.TLogger) {
				ts.V(2).Info("Creating a new Configuration", "configuration", names.Config)

				_, err := v1test.CreateConfiguration(t, clients, names, rtesting.WithConfigReadinessProbe(tt.ReadinessProbe))
				ts.FatalIfErr(err, "Failed to create Configuration", "configuration", names.Config)

				ts.V(1).Info("When the containers keep crashing, the Configuration should have error status.")

				err = v1test.WaitForConfigurationState(clients.ServingClient, names.Config, func(r *v1.Configuration) (bool, error) {
					cond := r.Status.GetCondition(v1.ConfigurationConditionReady)
					ts := ts.WithValues("configuration", names.Config, "condition", cond)
					v1test.ValidateCondition(ts, cond)
					if cond != nil && !cond.IsUnknown() {
						if cond.IsFalse() {
							return falseConfigurationValidator(ts, cond)
						}
						ts.Fatal("Configuration should not have become ready")
					}
					return false, nil
				}, "ConfigContainersCrashing")

				ts.FatalIfErr(err, "Failed to validate configuration state")

				revisionName, err := getRevisionFromConfiguration(clients, names.Config)
				ts.FatalIfErr(err, "Failed to get revision from configuration", "configuration", names.Config)

				ts.V(1).Info("When the containers keep crashing, the revision should have error status.")
				err = v1test.WaitForRevisionState(clients.ServingClient, revisionName, func(r *v1.Revision) (bool, error) {
					cond := r.Status.GetCondition(v1.RevisionConditionReady)
					ts := ts.WithValues("revision", revisionName, "condition", cond)
					v1test.ValidateCondition(ts, cond)
					if cond != nil {
						return revisionValidator(ts, cond)
					}
					return false, nil
				}, "RevisionContainersCrashing")

				ts.FatalIfErr(err, "Failed to validate revision state")
			})
		})
	}
}

// Get revision name from configuration.
func getRevisionFromConfiguration(clients *test.Clients, configName string) (string, error) {
	config, err := clients.ServingClient.Configs.Get(configName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if config.Status.LatestCreatedRevisionName != "" {
		return config.Status.LatestCreatedRevisionName, nil
	}
	return "", logging.Error("No valid revision name found", "configuration", configName)
}
