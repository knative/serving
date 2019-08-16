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
	"strings"

	"knative.dev/pkg/testutils/clustermanager/boskos"
	"knative.dev/pkg/testutils/common"
)

// GKEClient implements Client
type GKEClient struct {
}

// GKECluster implements ClusterOperations
type GKECluster struct {
	// Project might be GKE specific, so put it here
	Project *string
	// NeedCleanup tells whether the cluster needs to be deleted afterwards
	// This probably should be part of task wrapper's logic
	NeedCleanup bool
	// TODO: evaluate returning "google.golang.org/api/container/v1.Cluster" when implementing the creation logic
	Cluster *string
}

// Setup sets up a GKECluster client.
// numNodes: default to 3 if not provided
// nodeType: default to n1-standard-4 if not provided
// region: default to regional cluster if not provided, and use default backup regions
// zone: default is none, must be provided together with region
func (gs *GKEClient) Setup(numNodes *int, nodeType *string, region *string, zone *string, project *string) (ClusterOperations, error) {
	gc := &GKECluster{}
	// check for local run
	if nil != project { // if project is supplied, use it and create cluster
		gc.Project = project
		gc.NeedCleanup = true
	} else if err := gc.checkEnvironment(); nil != err {
		return nil, fmt.Errorf("failed checking existing cluster: '%v'", err)
	} else if nil != gc.Cluster { // return if Cluster was already set by kubeconfig
		return gc, nil
	}
	// check for Prow
	if common.IsProw() {
		project, err := boskos.AcquireGKEProject()
		if nil != err {
			return nil, fmt.Errorf("failed acquire boskos project: '%v'", err)
		}
		gc.Project = &project
	}
	if nil == gc.Project || "" == *gc.Project {
		return nil, fmt.Errorf("gcp project must be set")
	}
	return gc, nil
}

// Provider returns gke
func (gc *GKECluster) Provider() string {
	return "gke"
}

// Acquire gets existing cluster or create a new one
func (gc *GKECluster) Acquire() error {
	// Check if using existing cluster
	if nil != gc.Cluster {
		return nil
	}
	// TODO: Perform GKE specific cluster creation logics
	return nil
}

// Delete deletes a GKE cluster
func (gc *GKECluster) Delete() error {
	if !gc.NeedCleanup {
		return nil
	}
	// TODO: Perform GKE specific cluster deletion logics
	return nil
}

// checks for existing cluster by looking at kubeconfig,
// and sets up gc.Project and gc.Cluster properly, otherwise fail it.
// if project can be derived from gcloud, sets it up as well
func (gc *GKECluster) checkEnvironment() error {
	// if kubeconfig is configured, use it
	output, err := common.StandardExec("kubectl", "config", "current-context")
	if nil == err {
		// output should be in the form of gke_PROJECT_REGION_CLUSTER
		parts := strings.Split(string(output), "_")
		if len(parts) != 4 {
			return fmt.Errorf("kubectl current-context is malformed: '%s'", string(output))
		}
		gc.Project = &parts[1]
		gc.Cluster = &parts[3]
		return nil
	}
	if string(output) != "" {
		// this is unexpected error, should shout out directly
		return fmt.Errorf("failed running kubectl config current-context: '%s'", string(output))
	}

	// if gcloud is pointing to a project, use it
	output, err = common.StandardExec("gcloud", "config", "get-value", "project")
	if nil != err {
		return fmt.Errorf("failed getting gcloud project: '%v'", err)
	}
	if string(output) != "" {
		project := string(output)
		gc.Project = &project
	}

	return nil
}
