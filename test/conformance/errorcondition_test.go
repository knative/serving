// +build e2e

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

package conformance

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	containerMissing = "ContainerMissing"
)

// TestContainerErrorMsg is to validate the error condition defined at
// https://github.com/knative/serving/blob/master/docs/spec/errors.md
// for the container image missing scenario.
func TestContainerErrorMsg(t *testing.T) {
	if strings.HasSuffix(strings.Split(test.ServingFlags.DockerRepo, "/")[0], ".local") {
		t.Skip("Skipping for local docker repo")
	}
	clients := setup(t)

	//add test case specific name to its own logger
	logger := logging.GetContextLogger("TestContainerErrorMsg")

	names := test.ResourceNames{
		Config: test.AppendRandomString("test-container-error-msg", logger),
		Image:  "invalidhelloworld",
	}
	// Specify an invalid image path
	// A valid DockerRepo is still needed, otherwise will get UNAUTHORIZED instead of container missing error
	logger.Infof("Creating a new Configuration %s", names.Image)
	if _, err := test.CreateConfiguration(logger, clients, names, &test.Options{}); err != nil {
		t.Fatalf("Failed to create configuration %s", names.Config)
	}
	defer tearDown(clients, names)
	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)

	manifestUnknown := string(transport.ManifestUnknownErrorCode)
	logger.Infof("When the imagepath is invalid, the Configuration should have error status.")

	// Checking for "Container image not present in repository" scenario defined in error condition spec
	err := test.WaitForConfigurationState(clients.ServingClient, names.Config, func(r *v1alpha1.Configuration) (bool, error) {
		cond := r.Status.GetCondition(v1alpha1.ConfigurationConditionReady)
		if cond != nil && !cond.IsUnknown() {
			if strings.Contains(cond.Message, manifestUnknown) && cond.IsFalse() {
				return true, nil
			}
			logger.Infof("%s : %s : %s", cond.Reason, cond.Message, cond.Status)
			s := fmt.Sprintf("The configuration %s was not marked with expected error condition (Reason=\"%s\", Message=\"%s\", Status=\"%s\"), but with (Reason=\"%s\", Message=\"%s\", Status=\"%s\")", names.Config, containerMissing, manifestUnknown, "False", cond.Reason, cond.Message, cond.Status)
			return true, errors.New(s)
		}
		return false, nil
	}, "ContainerImageNotPresent")

	if err != nil {
		t.Fatalf("Failed to validate configuration state: %s", err)
	}

	revisionName, err := getRevisionFromConfiguration(clients, names.Config)
	if err != nil {
		t.Fatalf("Failed to get revision from configuration %s: %v", names.Config, err)
	}

	logger.Infof("When the imagepath is invalid, the revision should have error status.")
	err = test.WaitForRevisionState(clients.ServingClient, revisionName, func(r *v1alpha1.Revision) (bool, error) {
		cond := r.Status.GetCondition(v1alpha1.RevisionConditionReady)
		if cond != nil {
			if cond.Reason == containerMissing && strings.Contains(cond.Message, manifestUnknown) {
				return true, nil
			}
			return true, fmt.Errorf("The revision %s was not marked with expected error condition (Reason=%q, Message=%q), but with (Reason=%q, Message=%q)",
				revisionName, containerMissing, manifestUnknown, cond.Reason, cond.Message)
		}
		return false, nil
	}, "ImagePathInvalid")

	if err != nil {
		t.Fatalf("Failed to validate revision state: %s", err)
	}

	logger.Infof("When the revision has error condition, logUrl should be populated.")
	logURL, err := getLogURLFromRevision(clients, revisionName)
	if err != nil {
		t.Fatalf("Failed to get logUrl from revision %s: %v", revisionName, err)
	}

	// TODO(jessiezcc): actually validate the logURL, but requires kibana setup
	logger.Debugf("LogURL: %s", logURL)

	// TODO(jessiezcc): add the check to validate that Route is not marked as ready once https://github.com/knative/serving/issues/990 is fixed
}

// TestContainerExitingMsg is to validate the error condition defined at
// https://github.com/knative/serving/blob/master/docs/spec/errors.md
// for the container crashing scenario.
func TestContainerExitingMsg(t *testing.T) {
	clients := setup(t)

	//add test case specific name to its own logger
	logger := logging.GetContextLogger("TestContainerExitingMsg")

	names := test.ResourceNames{
		Config: test.AppendRandomString("test-container-exiting-msg", logger),
		Image:  "failing",
	}

	// The given image will always exit with an exit code of 5
	exitCodeReason := "ExitCode5"
	// ... and will print "Crashed..." before it exits
	errorLog := "Crashed..."

	logger.Infof("Creating a new Configuration %s", names.Image)

	// This probe is crucial for having a race free conformance test. It will prevent the
	// pod from becoming ready intermittently.
	probe := &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{},
		},
	}
	if _, err := test.CreateConfiguration(logger, clients, names, &test.Options{ReadinessProbe: probe}); err != nil {
		t.Fatalf("Failed to create configuration %s: %v", names.Config, err)
	}
	defer tearDown(clients, names)
	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)

	logger.Infof("When the containers keep crashing, the Configuration should have error status.")

	err := test.WaitForConfigurationState(clients.ServingClient, names.Config, func(r *v1alpha1.Configuration) (bool, error) {
		cond := r.Status.GetCondition(v1alpha1.ConfigurationConditionReady)
		if cond != nil && !cond.IsUnknown() {
			if strings.Contains(cond.Message, errorLog) && cond.IsFalse() {
				return true, nil
			}
			logger.Infof("%s : %s : %s", cond.Reason, cond.Message, cond.Status)
			return true, fmt.Errorf("The configuration %s was not marked with expected error condition (Reason=\"%s\", Message=\"%s\", Status=\"%s\"), but with (Reason=\"%s\", Message=\"%s\", Status=\"%s\")",
				names.Config, containerMissing, errorLog, "False", cond.Reason, cond.Message, cond.Status)
		}
		return false, nil
	}, "ConfigContainersCrashing")

	if err != nil {
		t.Fatalf("Failed to validate configuration state: %s", err)
	}

	revisionName, err := getRevisionFromConfiguration(clients, names.Config)
	if err != nil {
		t.Fatalf("Failed to get revision from configuration %s: %v", names.Config, err)
	}

	logger.Infof("When the containers keep crashing, the revision should have error status.")
	err = test.WaitForRevisionState(clients.ServingClient, revisionName, func(r *v1alpha1.Revision) (bool, error) {
		cond := r.Status.GetCondition(v1alpha1.RevisionConditionReady)
		if cond != nil {
			if cond.Reason == exitCodeReason && strings.Contains(cond.Message, errorLog) {
				return true, nil
			}
			return true, fmt.Errorf("The revision %s was not marked with expected error condition (Reason=%q, Message=%q), but with (Reason=%q, Message=%q)",
				revisionName, exitCodeReason, errorLog, cond.Reason, cond.Message)
		}
		return false, nil
	}, "RevisionContainersCrashing")

	if err != nil {
		t.Fatalf("Failed to validate revision state: %s", err)
	}

	logger.Infof("When the revision has error condition, logUrl should be populated.")
	_, err = getLogURLFromRevision(clients, revisionName)
	if err != nil {
		t.Fatalf("Failed to get logUrl from revision %s: %v", revisionName, err)
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
	return "", fmt.Errorf("No valid revision name found in configuration %s", configName)
}

// Get LogURL from revision.
func getLogURLFromRevision(clients *test.Clients, revisionName string) (string, error) {
	revision, err := clients.ServingClient.Revisions.Get(revisionName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if revision.Status.LogURL != "" && strings.Contains(revision.Status.LogURL, string(revision.GetUID())) {
		return revision.Status.LogURL, nil
	}
	return "", fmt.Errorf("The revision %s does't have valid logUrl: %s", revisionName, revision.Status.LogURL)
}
