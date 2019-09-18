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

	container "google.golang.org/api/container/v1beta1"
	"knative.dev/pkg/testutils/clustermanager/boskos"
	"knative.dev/pkg/testutils/common"

	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
)

const (
	DefaultGKEMinNodes = 1
	DefaultGKEMaxNodes = 3
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
	creationTimeout    = 20 * time.Minute
	deletionTimeout    = 10 * time.Minute
	autoscalingTimeout = 1 * time.Minute
)

// GKEClient implements Client
type GKEClient struct {
}

// GKERequest contains all requests collected for cluster creation
type GKERequest struct {
	MinNodes      int64
	MaxNodes      int64
	NodeType      string
	Region        string
	Zone          string
	BackupRegions []string
	Addons        []string
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
	delete(string, string, string) (*container.Operation, error)
	get(string, string, string) (*container.Cluster, error)
	getOperation(string, string, string) (*container.Operation, error)
	setAutoscaling(string, string, string, string, *container.SetNodePoolAutoscalingRequest) (*container.Operation, error)
}

// GKESDKClient Implement GKESDKOperations
type GKESDKClient struct {
	*container.Service
}

func (gsc *GKESDKClient) create(project, location string, rb *container.CreateClusterRequest) (*container.Operation, error) {
	parent := fmt.Sprintf("projects/%s/locations/%s", project, location)
	return gsc.Projects.Locations.Clusters.Create(parent, rb).Context(context.Background()).Do()
}

// delete deletes GKE cluster and waits until completion
func (gsc *GKESDKClient) delete(project, clusterName, location string) (*container.Operation, error) {
	parent := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", project, location, clusterName)
	return gsc.Projects.Locations.Clusters.Delete(parent).Context(context.Background()).Do()
}

func (gsc *GKESDKClient) get(project, location, cluster string) (*container.Cluster, error) {
	clusterFullPath := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", project, location, cluster)
	return gsc.Projects.Locations.Clusters.Get(clusterFullPath).Context(context.Background()).Do()
}

func (gsc *GKESDKClient) getOperation(project, location, opName string) (*container.Operation, error) {
	name := fmt.Sprintf("projects/%s/locations/%s/operations/%s", project, location, opName)
	return gsc.Service.Projects.Locations.Operations.Get(name).Do()
}

// setAutoscaling sets up autoscaling for a nodepool. This function is not
// covered by either `Clusters.Update` or `NodePools.Update`, so can not really
// make it as generic as the others
func (gsc *GKESDKClient) setAutoscaling(project, clusterName, location, nodepoolName string,
	rb *container.SetNodePoolAutoscalingRequest) (*container.Operation, error) {
	parent := fmt.Sprintf("projects/%s/locations/%s/clusters/%s/nodePools/%s", project, location, clusterName, nodepoolName)
	return gsc.Service.Projects.Locations.Clusters.NodePools.SetAutoscaling(parent, rb).Do()
}

// Setup sets up a GKECluster client.
// minNodes: default to 1 if not provided
// maxNodes: default to 3 if not provided
// nodeType: default to n1-standard-4 if not provided
// region: default to regional cluster if not provided, and use default backup regions
// zone: default is none, must be provided together with region
// project: no default
// addons: cluster addons to be added to cluster
func (gs *GKEClient) Setup(minNodes *int64, maxNodes *int64, nodeType *string, region *string, zone *string, project *string, addons []string) ClusterOperations {
	gc := &GKECluster{
		Request: &GKERequest{
			MinNodes:      DefaultGKEMinNodes,
			MaxNodes:      DefaultGKEMaxNodes,
			NodeType:      DefaultGKENodeType,
			Region:        DefaultGKERegion,
			Zone:          DefaultGKEZone,
			BackupRegions: DefaultGKEBackupRegions,
			Addons:        addons,
		},
	}

	if project != nil { // use provided project and create cluster
		gc.Project = project
		gc.NeedCleanup = true
	}

	if minNodes != nil {
		gc.Request.MinNodes = *minNodes
	}
	if maxNodes != nil {
		gc.Request.MaxNodes = *maxNodes
	}
	if nodeType != nil {
		gc.Request.NodeType = *nodeType
	}
	if region != nil {
		gc.Request.Region = *region
	}
	if common.GetOSEnv(regionEnv) != "" {
		gc.Request.Region = common.GetOSEnv(regionEnv)
	}
	if common.GetOSEnv(backupRegionEnv) != "" {
		gc.Request.BackupRegions = strings.Split(common.GetOSEnv(backupRegionEnv), " ")
	}
	if zone != nil {
		gc.Request.Zone = *zone
		gc.Request.BackupRegions = make([]string, 0)
	}

	ctx := context.Background()
	c, err := google.DefaultClient(ctx, container.CloudPlatformScope)
	if err != nil {
		log.Fatalf("failed create google client: '%v'", err)
	}

	containerService, err := container.New(c)
	if err != nil {
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
	if gc.Project == nil {
		if err := gc.checkEnvironment(); err != nil {
			return fmt.Errorf("failed checking existing cluster: '%v'", err)
		} else if gc.Cluster != nil { // return if Cluster was already set by kubeconfig
			return nil
		}
	}
	// Get project name from boskos if running in Prow
	if gc.Project == nil && common.IsProw() {
		project, err := gc.boskosOps.AcquireGKEProject(nil)
		if err != nil {
			return fmt.Errorf("failed acquire boskos project: '%v'", err)
		}
		gc.Project = &project.Name
	}
	if gc.Project == nil || *gc.Project == "" {
		return errors.New("gcp project must be set")
	}
	if !common.IsProw() && gc.Cluster == nil {
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
	if gc.Cluster != nil {
		return nil
	}
	// Perform GKE specific cluster creation logics
	clusterName, err := getResourceName(ClusterResource)
	if err != nil {
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
	var op *container.Operation
	for i, region := range regions {
		// Restore innocence
		err = nil
		rb := &container.CreateClusterRequest{
			Cluster: &container.Cluster{
				Name: clusterName,
				// Installing addons after cluster creation takes at least 5
				// minutes, so install addons as part of cluster creation, which
				// doesn't seem to add much time on top of cluster creation
				AddonsConfig:     gc.getAddonsConfig(),
				InitialNodeCount: gc.Request.MinNodes,
				NodeConfig: &container.NodeConfig{
					MachineType: gc.Request.NodeType,
				},
			},
			ProjectId: *gc.Project,
		}

		clusterLoc := getClusterLocation(region, gc.Request.Zone)

		// Deleting cluster if it already exists
		existingCluster, _ := gc.operations.get(*gc.Project, clusterLoc, clusterName)
		if existingCluster != nil {
			log.Printf("Cluster %q already exists in %q. Deleting...", clusterName, clusterLoc)
			op, err = gc.operations.delete(*gc.Project, clusterName, clusterLoc)
			if err == nil {
				err = gc.wait(clusterLoc, op.Name, deletionTimeout)
			}
		}
		// Creating cluster only if previous step succeeded
		if err == nil {
			log.Printf("Creating cluster %q in %q", clusterName, clusterLoc)
			op, err = gc.operations.create(*gc.Project, clusterLoc, rb)
			if err == nil {
				if err = gc.wait(clusterLoc, op.Name, creationTimeout); err == nil {
					cluster, err = gc.operations.get(*gc.Project, clusterLoc, rb.Cluster.Name)
				}
			}
			if err == nil { // Enable autoscaling and set limits
				arb := &container.SetNodePoolAutoscalingRequest{
					Autoscaling: &container.NodePoolAutoscaling{
						Enabled:      true,
						MinNodeCount: gc.Request.MinNodes,
						MaxNodeCount: gc.Request.MaxNodes,
					},
				}

				op, err = gc.operations.setAutoscaling(*gc.Project, clusterName, clusterLoc, "default-pool", arb)
				if err == nil {
					err = gc.wait(clusterLoc, op.Name, autoscalingTimeout)
				}
			}
		}
		if err != nil {
			errMsg := fmt.Sprintf("Error during cluster creation: '%v'. ", err)
			if gc.NeedCleanup { // Delete half created cluster if it's user created
				errMsg = fmt.Sprintf("%sDeleting cluster %q in %q in background...\n", errMsg, clusterName, clusterLoc)
				go gc.operations.delete(*gc.Project, clusterName, clusterLoc)
			}
			// Retry another region if cluster creation failed.
			// TODO(chaodaiG): catch specific errors as we know what the error look like for stockout etc.
			if len(regions) != i+1 {
				errMsg = fmt.Sprintf("%sRetry another region %q for cluster creation", errMsg, regions[i+1])
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

// Delete takes care of GKE cluster resource cleanup. It only release Boskos resource if running in
// Prow, otherwise deletes the cluster if marked NeedsCleanup
func (gc *GKECluster) Delete() error {
	gc.ensureProtected()
	// Release Boskos if running in Prow, will let Janitor taking care of
	// clusters deleting
	if common.IsProw() {
		log.Printf("Releasing Boskos resource: '%v'", *gc.Project)
		return gc.boskosOps.ReleaseGKEProject(nil, *gc.Project)
	}

	// NeedCleanup is only true if running locally and cluster created by the
	// process
	if !gc.NeedCleanup {
		return nil
	}
	// Should only get here if running locally and cluster created by this
	// client, so at this moment cluster should have been set
	if gc.Cluster == nil {
		return fmt.Errorf("cluster doesn't exist")
	}

	log.Printf("Deleting cluster %q in %q", gc.Cluster.Name, gc.Cluster.Location)
	op, err := gc.operations.delete(*gc.Project, gc.Cluster.Name, gc.Cluster.Location)
	if err == nil {
		err = gc.wait(gc.Cluster.Location, op.Name, deletionTimeout)
	}
	if err != nil {
		return fmt.Errorf("failed deleting cluster: '%v'", err)
	}
	return nil
}

// getAddonsConfig gets AddonsConfig from Request, contains the logic of
// converting string argument to typed AddonsConfig, for example `IstioConfig`.
// Currently supports istio
func (gc *GKECluster) getAddonsConfig() *container.AddonsConfig {
	const (
		// Define all supported addons here
		istio = "istio"
	)
	ac := &container.AddonsConfig{}
	for _, name := range gc.Request.Addons {
		switch strings.ToLower(name) {
		case istio:
			ac.IstioConfig = &container.IstioConfig{Disabled: false}
		default:
			panic(fmt.Sprintf("addon type %q not supported. Has to be one of: %q", name, istio))
		}
	}

	return ac
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
				if err == nil {
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
			if err != nil {
				return err
			}
		}
	}

	return err
}

// ensureProtected ensures not operating on protected project/cluster
func (gc *GKECluster) ensureProtected() {
	if gc.Project != nil {
		for _, pp := range protectedProjects {
			if *gc.Project == pp {
				log.Fatalf("project %q is protected", *gc.Project)
			}
		}
	}
	if gc.Cluster != nil {
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
	if err == nil {
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
				if err != nil {
					return fmt.Errorf("couldn't find cluster %s in %s in %s, does it exist? %v", parts[3], parts[1], parts[2], err)
				}
				return nil
			}
		}
	}
	if err != nil && len(output) > 0 {
		// this is unexpected error, should shout out directly
		return fmt.Errorf("failed running kubectl config current-context: '%s'", string(output))
	}

	// if gcloud is pointing to a project, use it
	output, err = common.StandardExec("gcloud", "config", "get-value", "project")
	if err != nil {
		return fmt.Errorf("failed getting gcloud project: '%v'", err)
	}
	if string(output) != "" {
		project := strings.Trim(strings.TrimSpace(string(output)), "\n\r")
		gc.Project = &project
	}

	return nil
}
