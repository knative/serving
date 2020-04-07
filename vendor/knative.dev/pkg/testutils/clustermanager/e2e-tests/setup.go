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
	"log"
	"strings"

	container "google.golang.org/api/container/v1beta1"

	"knative.dev/pkg/test/gke"
	"knative.dev/pkg/testutils/clustermanager/e2e-tests/boskos"
	"knative.dev/pkg/testutils/clustermanager/e2e-tests/common"
)

// GKEClient implements Client
type GKEClient struct {
}

// GKERequest contains all requests collected for cluster creation
type GKERequest struct {
	// Request holds settings for GKE native operations
	gke.Request

	// BackupRegions: fall back regions to try out in case of cluster creation
	// failure due to regional issue(s)
	BackupRegions []string

	// SkipCreation: skips cluster creation
	SkipCreation bool

	// ResourceType: the boskos resource type to acquire to hold the cluster in create
	ResourceType string
}

// GKECluster implements ClusterOperations
type GKECluster struct {
	Request *GKERequest
	// Project might be GKE specific, so put it here
	Project string
	Cluster *container.Cluster

	// isBoskos is true if the GCP project used is managed by boskos
	isBoskos bool
	// asyncCleanup tells whether the cluster needs to be deleted asynchronously afterwards
	// It should be true on Prow but false on local.
	asyncCleanup bool
	operations   gke.SDKOperations
	boskosOps    boskos.Operation
}

// Setup sets up a GKECluster client, takes GEKRequest as parameter and applies
// all defaults if not defined.
func (gs *GKEClient) Setup(r GKERequest) ClusterOperations {
	gc := &GKECluster{}

	if r.Project != "" { // use provided project to create cluster
		gc.Project = r.Project
	} else if common.IsProw() { // if no project is provided and is on Prow, use boskos
		gc.isBoskos = true
		gc.asyncCleanup = true
	}

	if r.MinNodes == 0 {
		r.MinNodes = defaultGKEMinNodes
	}
	if r.MaxNodes == 0 {
		r.MaxNodes = defaultGKEMaxNodes
		// We don't want MaxNodes < MinNodes
		if r.MinNodes > r.MaxNodes {
			r.MaxNodes = r.MinNodes
		}
	}
	if r.NodeType == "" {
		r.NodeType = defaultGKENodeType
	}
	// Only use default backup regions if region is not provided
	if len(r.BackupRegions) == 0 && r.Region == "" {
		r.BackupRegions = defaultGKEBackupRegions
		if common.GetOSEnv(backupRegionEnv) != "" {
			r.BackupRegions = strings.Split(common.GetOSEnv(backupRegionEnv), " ")
		}
	}
	if r.Region == "" {
		r.Region = defaultGKERegion
		if common.GetOSEnv(regionEnv) != "" {
			r.Region = common.GetOSEnv(regionEnv)
		}
	}
	if r.Zone == "" {
		r.Zone = defaultGKEZone
	} else { // No backupregions if zone is provided
		r.BackupRegions = make([]string, 0)
	}

	loc := gke.GetClusterLocation(r.Region, r.Zone)
	if version, err := resolveGKEVersion(r.GKEVersion, loc); err == nil {
		r.GKEVersion = version
	} else {
		log.Fatalf("Failed to resolve GKE version: '%v'", err)
	}

	if r.ResourceType == "" {
		r.ResourceType = defaultResourceType
	}

	gc.Request = &r

	client, err := boskos.NewClient("", /* boskos owner */
		"", /* boskos user */
		"" /* boskos password file */)
	if err != nil {
		log.Fatalf("Failed to create boskos client: '%v'", err)
	}
	gc.boskosOps = client

	return gc
}
