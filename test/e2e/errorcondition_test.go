/*
Copyright 2018 Google Inc. All Rights Reserved.
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
	"errors"
	"log"
	"strings"
	"testing"
	//"time"
	"fmt"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"github.com/google/go-containerregistry/v1/remote"
)

const (
	containerMissing = "ContainerMissing"

)

//This test is to validate error condition defined at
//https://github.com/knative/serving/blob/master/docs/spec/errors.md
// for container image missing scenario
func TestContainerErrorMsg(t *testing.T) {
	clients := Setup(t)
	defer TearDown(clients)
	test.CleanupOnInterrupt(func() { TearDown(clients) })

	var imagePath string
	//specify an invalid image path
	imagePath = strings.Join([]string{test.Flags.DockerRepo, "invalidhelloworld"}, "/")

	log.Printf("Creating a new Route and Configuration %s", imagePath)
	err := CreateRouteAndConfig(clients, imagePath)
	if err != nil {
		t.Fatalf("Failed to create Route and Configuration: %v", err)
	}
	manifestUnknown := string(remote.ManifestUnknownErrorCode) //"MANIFEST_UNKNOWN"
	log.Println("When the imagepath is invalid, the Configuration should have error status.")

	//checking for "Container image not present in repository" scenario defined in error condition spec
	err = test.WaitForConfigurationState(clients.Configs, ConfigName, func(r *v1alpha1.Configuration) (bool, error) {
		cond := r.Status.GetCondition(v1alpha1.ConfigurationConditionLatestRevisionReady)
		if cond != nil {
			if cond.Reason == containerMissing && strings.HasPrefix(cond.Message, manifestUnknown) && cond.Status == "False" {
				return true, nil
			} else {
				s := fmt.Sprintf("The configuration %s was not marked with expected error condition, Reason: %s Message: %s Status: %s", ConfigName, containerMissing, manifestUnknown, "False")
				return true, errors.New(s)
			}
		} else {
			return false, nil
		}
	})

	if err != nil {
		t.Fatalf("%v", err)
	}

	revisionName, err := GetRevisionFromConfiguration(clients, ConfigName)
	if err != nil {
		t.Fatalf("%v", err)
	}

	log.Println("When the imagepath is invalid, the revision should have error status.")
	err = test.WaitForRevisionState(clients.Revisions, revisionName, func(r *v1alpha1.Revision) (bool, error) {
		cond := r.Status.GetCondition(v1alpha1.RevisionConditionReady)
		if cond != nil {
			return cond.Reason == containerMissing && strings.HasPrefix(cond.Message, manifestUnknown), nil
		} else {
			return false, nil
		}
	})

	if err != nil {
		t.Fatalf("The revision %s was not marked with expected error condition Reason: %s Message: %s", revisionName, containerMissing, manifestUnknown)
	}

	log.Println("when the revision has error condition, logUrl should be populated.")
	logURL, err := GetLogUrlFromRevision(clients, revisionName)
	if err != nil {
		t.Fatalf("%v", err)
	}

	//ToDo: actually validate the logURL, but requires kibana setup
	log.Printf("LogURL: %s", logURL)

	/*  ToDo:
	Add the check to validate that Route is not marked as ready once https://github.com/elafros/elafros/issues/990 is fixed
	*/
}

func GetRevisionFromConfiguration(clients *test.Clients, configName string) (string, error) {
	config, err := clients.Configs.Get(configName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if config.Status.LatestCreatedRevisionName != "" {
		return config.Status.LatestCreatedRevisionName, nil
	} else {
		s := fmt.Sprintf("No valid revision name found in configuration %s", configName)
		return "", errors.New(s)
	}
}

func GetLogUrlFromRevision(clients *test.Clients, revisionName string) (string, error) {
	revision, err := clients.Revisions.Get(revisionName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if revision.Status.LogURL != "" && strings.Contains(revision.Status.LogURL, string(revision.GetUID())) {
		return revision.Status.LogURL, nil
	} else {
		s := fmt.Sprintf("The revision %s does't have valid logUrl: %s", revisionName, revision.Status.LogURL)
		return "", errors.New(s)
	}
}
