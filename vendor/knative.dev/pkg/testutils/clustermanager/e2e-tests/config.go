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

package clustermanager

import (
	"regexp"

	"knative.dev/pkg/testutils/clustermanager/e2e-tests/boskos"
)

const (
	defaultGKEMinNodes  = 1
	defaultGKEMaxNodes  = 3
	defaultGKENodeType  = "e2-standard-4"
	defaultGKERegion    = "us-central1"
	defaultGKEZone      = ""
	regionEnv           = "E2E_CLUSTER_REGION"
	backupRegionEnv     = "E2E_CLUSTER_BACKUP_REGIONS"
	defaultResourceType = boskos.GKEProjectResource

	clusterRunning = "RUNNING"
)

var (
	protectedProjects       = []string{"knative-tests"}
	protectedClusters       = []string{"knative-prow"}
	defaultGKEBackupRegions = []string{"us-west1", "us-east1"}

	// If one of the error patterns below is matched, it would be recommended to
	// retry creating the cluster in a different region/zone.
	// - stockout (https://github.com/knative/test-infra/issues/592)
	// - latest GKE not available in this region/zone yet (https://github.com/knative/test-infra/issues/694)
	retryableCreationErrors = []*regexp.Regexp{
		regexp.MustCompile(".*Master version \"[0-9a-z\\-.]+\" is unsupported.*"),
		regexp.MustCompile(".*No valid versions with the prefix \"[0-9.]+\" found.*"),
		regexp.MustCompile(".*does not have enough resources available to fulfill.*"),
		regexp.MustCompile(".*only \\d+ nodes out of \\d+ have registered; this is likely due to Nodes failing to start correctly.*"),
	}
)
