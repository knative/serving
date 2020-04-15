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

package clustermanager

import (
	"fmt"
	"log"
	"strings"

	"knative.dev/pkg/test/cmd"
	"knative.dev/pkg/testutils/clustermanager/e2e-tests/common"
)

const (
	defaultGKEVersion = "default"
	latestGKEVersion  = "latest"
)

var (
	ClusterResource ResourceType = "e2e-cls"
)

type ResourceType string

// getResourceName defines how a resource should be named based on it's
// type, the name follows: k{reponame}-{typename} for local user, and
// append BUILD_NUMBER with up to 20 chars. This is best effort, as it
// shouldn't fail
func getResourceName(rt ResourceType) (string, error) {
	var resName string
	repoName, err := common.GetRepoName()
	if err != nil {
		return "", fmt.Errorf("failed getting reponame for forming resource name: '%v'", err)
	}
	resName = fmt.Sprintf("k%s-%s", repoName, string(rt))
	if common.IsProw() {
		buildNumStr := common.GetOSEnv("BUILD_NUMBER")
		if buildNumStr == "" {
			return "", fmt.Errorf("failed getting BUILD_NUMBER env var")
		}
		if len(buildNumStr) > 20 {
			buildNumStr = buildNumStr[:20]
		}
		resName = fmt.Sprintf("%s-%s", resName, buildNumStr)
	}
	return resName, nil
}

// resolveGKEVersion returns the actual GKE version based on the raw version and location
func resolveGKEVersion(raw, location string) (string, error) {
	switch raw {
	case defaultGKEVersion:
		defaultCmd := "gcloud container get-server-config --format='value(defaultClusterVersion)' --zone=" + location
		version, err := cmd.RunCommand(defaultCmd)
		if err != nil && version != "" {
			return "", fmt.Errorf("failed getting the default version: %w", err)
		}
		log.Printf("Using default version, %s", version)
		return version, nil
	case latestGKEVersion:
		validCmd := "gcloud container get-server-config --format='value(validMasterVersions)' --zone=" + location
		versionsStr, err := cmd.RunCommand(validCmd)
		if err != nil && versionsStr != "" {
			return "", fmt.Errorf("failed getting the list of valid versions: %w", err)
		}
		versions := strings.Split(versionsStr, ";")
		log.Printf("Using the latest version, %s", strings.TrimSpace(versions[0]))
		return strings.TrimSpace(versions[0]), nil
	default:
		log.Printf("Using the custom version, %s", raw)
		return raw, nil
	}
}
