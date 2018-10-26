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
	"fmt"
	"regexp"
	"testing"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
)

func getImageDigest(clients *test.Clients, names test.ResourceNames) (string, error) {
	var imageDigest string
	err := test.WaitForRevisionState(clients.ServingClient, names.Revision, func(r *v1alpha1.Revision) (bool, error) {
		if r.Status.ImageDigest != "" {
			imageDigest = r.Status.ImageDigest
			return true, nil
		}
		return false, nil
	}, "RevisionUpdatedWithImageDigest")
	return imageDigest, err
}

func assertIsDigestForImage(t *testing.T, imageName string, imageDigest string) {
	imageDigestRegex := fmt.Sprintf("%s/%s@sha256:[0-9a-f]{64}", test.ServingFlags.DockerRepo, imageName)
	match, err := regexp.MatchString(imageDigestRegex, imageDigest)
	if err != nil {
		t.Fatalf("Unable to compare regex %s to digest %s", imageDigestRegex, imageDigest)
	}
	if !match {
		t.Fatalf("Image Digest %s does not match regex %s", imageDigest, imageDigestRegex)
	}
}

func TestRevisionCreation(t *testing.T) {
	clients := setup(t)

	logger := logging.GetContextLogger("TestRevisionCreation")

	imageName := pizzaPlanet1
	imagePath := test.ImagePath(imageName)

	names := test.ResourceNames{
		Config:        test.AppendRandomString("prod", logger),
		Route:         test.AppendRandomString("pizzaplanet", logger),
		TrafficTarget: test.AppendRandomString("pizzaplanet", logger),
	}

	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)
	defer tearDown(clients, names)

	logger.Infof("Creating a new Configuration")
	err := test.CreateConfiguration(logger, clients, names, imagePath, &test.Options{})
	if err != nil {
		t.Fatalf("Failed to create Configuration: %v", err)
	}

	logger.Infof("Creating a new Route")
	err = test.CreateRoute(logger, clients, names)
	if err != nil {
		t.Fatalf("Failed to create Route: %v", err)
	}

	logger.Infof("The Configuration will be updated with the name of the Revision once it is created")
	revisionName, err := getNextRevisionName(clients, names)
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the new revision: %v", names.Config, err)
	}
	names.Revision = revisionName

	logger.Infof("The Revision will be updated with the digest of the image once it is created")
	imageDigest, err := getImageDigest(clients, names)
	if err != nil {
		t.Fatalf("Revision %s was not updated with the image digest: %v", names.Revision, err)
	}

	logger.Infof("The image digest should be for the given image")
	assertIsDigestForImage(t, imageName, imageDigest)
}
