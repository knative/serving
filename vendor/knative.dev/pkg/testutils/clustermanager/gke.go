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
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"knative.dev/pkg/testutils/clustermanager/boskos"
	"knative.dev/pkg/testutils/common"

	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/container/v1"
)

const (
	DefaultGKENumNodes = 1
	DefaultGKENodeType = "n1-standard-4"
	DefaultGKERegion   = "us-central1"
	DefaultGKEZone     = ""
	regionEnv          = "E2E_CLUSTER_REGION"
	backupRegionEnv    = "E2E_CLUSTER_BACKUP_REGIONS"
)

var (
	DefaultGKEBackupRegions = []string{"us-west1", "us-east1"}
	protectedProjects       = []string{"knative-tests"}
	protectedClusters       = []string{"knative-prow"}
	// These are arbitrary numbers determined based on past experience
	creationTimeout = 20 * time.Minute
)

// GKEClient implements Client
type GKEClient struct {
}

// GKERequest contains all requests collected for cluster creation
type GKERequest struct {
	NumNodes      int64
	NodeType      string
	Region        string
	Zone          string
	BackupRegions []string
}

// GKECluster implements ClusterOperations
type GKECluster struct {
	Request *GKERequest
	// Project might be GKE specific, so put it here
	Project *string
	// NeedCleanup tells whether the cluster needs to be deleted afterwards
	// This probably should be part of task wrapper's logic
	NeedCleanup bool
	Cluster     *container.Cluster
	operations  GKESDKOperations
	boskosOps   boskos.Operation
}

// GKESDKOperations wraps GKE SDK related functions
type GKESDKOperations interface {
	create(string, string, *container.CreateClusterRequest) (*container.Operation, error)
	get(string, string, string) (*container.Cluster, error)
	getOperation(string, string, string) (*container.Operation, error)
}

// GKESDKClient Implement GKESDKOperations
type GKESDKClient struct {
	*container.Service
}

func (gsc *GKESDKClient) create(project, location string, rb *container.CreateClusterRequest) (*container.Operation, error) {
	parent := fmt.Sprintf("projects/%s/locations/%s", project, location)
	return gsc.Projects.Locations.Clusters.Create(parent, rb).Context(context.Background()).Do()
}

func (gsc *GKESDKClient) get(project, location, cluster string) (*container.Cluster, error) {
	clusterFullPath := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", project, location, cluster)
	return gsc.Projects.Locations.Clusters.Get(clusterFullPath).Context(context.Background()).Do()
}

func (gsc *GKESDKClient) getOperation(project, location, opName string) (*container.Operation, error) {
	name := fmt.Sprintf("projects/%s/locations/%s/operations/%s", project, location, opName)
	return gsc.Service.Projects.Locations.Operations.Get(name).Do()
}

// Setup sets up a GKECluster client.
// numNodes: default to 3 if not provided
// nodeType: default to n1-standard-4 if not provided
// region: default to regional cluster if not provided, and use default backup regions
// zone: default is none, must be provided together with region
func (gs *GKEClient) Setup(numNodes *int64, nodeType *string, region *string, zone *string, project *string) ClusterOperations {
	gc := &GKECluster{
		Request: &GKERequest{
			NumNodes:      DefaultGKENumNodes,
			NodeType:      DefaultGKENodeType,
			Region:        DefaultGKERegion,
			Zone:          DefaultGKEZone,
			BackupRegions: DefaultGKEBackupRegions},
	}

	if nil != project { // use provided project and create cluster
		gc.Project = project
		gc.NeedCleanup = true
	}

	if nil != numNodes {
		gc.Request.NumNodes = *numNodes
	}
	if nil != nodeType {
		gc.Request.NodeType = *nodeType
	}
	if nil != region {
		gc.Request.Region = *region
	}
	if "" != common.GetOSEnv(regionEnv) {
		gc.Request.Region = common.GetOSEnv(regionEnv)
	}
	if "" != common.GetOSEnv(backupRegionEnv) {
		gc.Request.BackupRegions = strings.Split(common.GetOSEnv(backupRegionEnv), " ")
	}
	if nil != zone {
		gc.Request.Zone = *zone
		gc.Request.BackupRegions = make([]string, 0)
	}

	ctx := context.Background()
	c, err := google.DefaultClient(ctx, container.CloudPlatformScope)
	if nil != err {
		log.Fatalf("failed create google client: '%v'", err)
	}

	containerService, err := container.New(c)
	if nil != err {
		log.Fatalf("failed create container service: '%v'", err)
	}
	gc.operations = &GKESDKClient{containerService}

	gc.boskosOps = &boskos.Client{}

	return gc
}

// Initialize sets up GKE SDK client, checks environment for cluster and
// projects to decide whether use existing cluster/project or creating new ones.
func (gc *GKECluster) Initialize() error {
	// Try obtain project name via `kubectl`, `gcloud`
	if nil == gc.Project {
		if err := gc.checkEnvironment(); nil != err {
			return fmt.Errorf("failed checking existing cluster: '%v'", err)
		} else if nil != gc.Cluster { // return if Cluster was already set by kubeconfig
			return nil
		}
	}
	// Get project name from boskos if running in Prow
	if nil == gc.Project && common.IsProw() {
		project, err := gc.boskosOps.AcquireGKEProject(nil)
		if nil != err {
			return fmt.Errorf("failed acquire boskos project: '%v'", err)
		}
		gc.Project = &project.Name
	}
	if nil == gc.Project || "" == *gc.Project {
		return errors.New("gcp project must be set")
	}
	if !common.IsProw() && nil == gc.Cluster {
		gc.NeedCleanup = true
	}
	log.Printf("Using project %q for running test", *gc.Project)
	return nil
}

// Provider returns gke
func (gc *GKECluster) Provider() string {
	return "gke"
}

// Acquire gets existing cluster or create a new one, the creation logic
// contains retries in BackupRegions. Default creating cluster
// in us-central1, and default BackupRegions are us-west1 and us-east1. If
// Region or Zone is provided then there is no retries
func (gc *GKECluster) Acquire() error {
	gc.ensureProtected()
	var err error
	// Check if using existing cluster
	if nil != gc.Cluster {
		return nil
	}
	// Perform GKE specific cluster creation logics
	clusterName, err := getResourceName(ClusterResource)
	if nil != err {
		return fmt.Errorf("failed getting cluster name: '%v'", err)
	}

	regions := []string{gc.Request.Region}
	for _, br := range gc.Request.BackupRegions {
		exist := false
		for _, region := range regions {
			if br == region {
				exist = true
			}
		}
		if !exist {
			regions = append(regions, br)
		}
	}
	var cluster *container.Cluster
	for i, region := range regions {
		rb := &container.CreateClusterRequest{
			Cluster: &container.Cluster{
				Name:             clusterName,
				InitialNodeCount: gc.Request.NumNodes,
				NodeConfig: &container.NodeConfig{
					MachineType: gc.Request.NodeType,
				},
			},
			ProjectId: *gc.Project,
		}

		clusterLoc := getClusterLocation(region, gc.Request.Zone)
		// TODO(chaodaiG): add deleting logic once cluster deletion logic is done

		log.Printf("Creating cluster %q' in %q", clusterName, clusterLoc)
		var createOp *container.Operation
		createOp, err = gc.operations.create(*gc.Project, clusterLoc, rb)
		if nil == err {
			if err = gc.wait(clusterLoc, createOp.Name, creationTimeout); nil == err {
				cluster, err = gc.operations.get(*gc.Project, clusterLoc, rb.Cluster.Name)
			}
		}
		if nil != err {
			errMsg := fmt.Sprintf("error creating cluster: '%v'", err)
			if gc.NeedCleanup { // Delete half created cluster if it's user created
				// TODO(chaodaiG): add this part when deletion logic is done
			}
			// TODO(chaodaiG): catch specific errors as we know what the error look like for stockout etc.
			if len(regions) != i+1 {
				errMsg = fmt.Sprintf("%s. Retry another region '%s' for cluster creation", errMsg, regions[i+1])
			}
			log.Printf(errMsg)
		} else {
			log.Print("Cluster creation completed")
			gc.Cluster = cluster
			break
		}
	}

	return err
}

// wait depends on unique opName(operation ID created by cloud), and waits until
// it's done
func (gc *GKECluster) wait(location, opName string, wait time.Duration) error {
	const (
		pendingStatus = "PENDING"
		runningStatus = "RUNNING"
		doneStatus    = "DONE"
	)
	var op *container.Operation
	var err error

	timeout := time.After(wait)
	tick := time.Tick(500 * time.Millisecond)
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			return errors.New("timed out waiting")
		case <-tick:
			// Retry 3 times in case of weird network error, or rate limiting
			for r, w := 0, 50*time.Microsecond; r < 3; r, w = r+1, w*2 {
				op, err = gc.operations.getOperation(*gc.Project, location, opName)
				if nil == err {
					if op.Status == doneStatus {
						return nil
					} else if op.Status == pendingStatus || op.Status == runningStatus {
						// Valid operation, no need to retry
						break
					} else {
						// Have seen intermittent error state and fixed itself,
						// let it retry to avoid too much flakiness
						err = fmt.Errorf("unexpected operation status: %q", op.Status)
					}
				}
				time.Sleep(w)
			}
			// If err still persist after retries, exit
			if nil != err {
				return err
			}
		}
	}

	return err
}

// ensureProtected ensures not operating on protected project/cluster
func (gc *GKECluster) ensureProtected() {
	if nil != gc.Project {
		for _, pp := range protectedProjects {
			if *gc.Project == pp {
				log.Fatalf("project %q is protected", *gc.Project)
			}
		}
	}
	if nil != gc.Cluster {
		for _, pc := range protectedClusters {
			if gc.Cluster.Name == pc {
				log.Fatalf("cluster %q is protected", gc.Cluster.Name)
			}
		}
	}
}

// checks for existing cluster by looking at kubeconfig,
// and sets up gc.Project and gc.Cluster properly, otherwise fail it.
// if project can be derived from gcloud, sets it up as well
func (gc *GKECluster) checkEnvironment() error {
	var err error
	// if kubeconfig is configured, use it
	output, err := common.StandardExec("kubectl", "config", "current-context")
	if nil == err {
		currentContext := strings.TrimSpace(string(output))
		if strings.HasPrefix(currentContext, "gke_") {
			// output should be in the form of gke_PROJECT_REGION_CLUSTER
			parts := strings.Split(currentContext, "_")
			if len(parts) != 4 { // fall through with warning
				log.Printf("WARNING: ignoring kubectl current-context since it's malformed: '%s'", currentContext)
			} else {
				log.Printf("kubeconfig isn't empty, uses this cluster for running tests: %s", currentContext)
				gc.Project = &parts[1]
				gc.Cluster, err = gc.operations.get(*gc.Project, parts[2], parts[3])
				if nil != err {
					return fmt.Errorf("couldn't find cluster %s in %s in %s, does it exist? %v", parts[3], parts[1], parts[2], err)
				}
				return nil
			}
		}
	}
	if nil != err && len(output) > 0 {
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
