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

	"github.com/davecgh/go-spew/spew"
	container "google.golang.org/api/container/v1beta1"
	"google.golang.org/api/option"

	"knative.dev/pkg/test/gke"
	"knative.dev/pkg/testutils/clustermanager/e2e-tests/common"
)

// Provider returns gke
func (gc *GKECluster) Provider() string {
	return "gke"
}

// Acquire gets existing cluster or create a new one, the creation logic
// contains retries in BackupRegions. Default creating cluster
// in us-central1, and default BackupRegions are us-west1 and us-east1. If
// Region or Zone is provided then there is no retries
func (gc *GKECluster) Acquire() error {
	if gc.Request.SkipCreation {
		if err := gc.checkEnvironment(); err != nil {
			return fmt.Errorf("failed checking project/cluster from environment: '%w'", err)
		}
		// If gc.Cluster is discovered above, then the cluster exists and it's
		// project and name matches with requested, use it
		if gc.Cluster != nil {
			gc.ensureProtected()
			return nil
		}
		return errors.New("failing acquiring an existing cluster")
	}

	// If running on Prow and project name is not provided, get project name from boskos.
	if gc.Request.Project == "" && gc.isBoskos {
		project, err := gc.boskosOps.AcquireGKEProject(gc.Request.ResourceType)
		if err != nil {
			return fmt.Errorf("failed acquiring boskos project: '%w'", err)
		}
		gc.Project = project.Name
	}
	if gc.Project == "" {
		return errors.New("GCP project must be set")
	}
	gc.ensureProtected()
	log.Printf("Identified project %s for cluster creation", gc.Project)

	client, err := gc.newGKEClient(gc.Project)
	if err != nil {
		return fmt.Errorf("failed creating the GKE client: '%w'", err)
	}
	// Make a deep copy of the request struct, since the original request is supposed to be immutable
	request := gc.Request.DeepCopy()
	// We are going to use request for creating cluster, set its Project
	request.Project = gc.Project
	// Set the cluster name if it doesn't exist
	if request.ClusterName == "" {
		var err error
		request.ClusterName, err = getResourceName(ClusterResource)
		if err != nil {
			log.Fatalf("Failed getting cluster name: '%v'", err)
		}
	}

	// Combine Region with BackupRegions, these will be the regions used for
	// retrying creation logic
	regions := []string{request.Region}
	for _, br := range gc.Request.BackupRegions {
		if br != request.Region {
			regions = append(regions, br)
		}
	}
	var cluster *container.Cluster
	rb, err := gke.NewCreateClusterRequest(request)
	if err != nil {
		return fmt.Errorf("failed building the CreateClusterRequest: '%w'", err)
	}
	for i, region := range regions {
		// Restore innocence
		err = nil

		clusterName := request.ClusterName
		// Use cluster if it already exists and running
		existingCluster, _ := client.GetCluster(gc.Project, region, request.Zone, clusterName)
		if existingCluster != nil && existingCluster.Status == clusterRunning {
			gc.Cluster = existingCluster
			return nil
		}
		// Creating cluster
		log.Printf("Creating cluster %q in region %q zone %q with:\n%+v", clusterName, region, request.Zone, spew.Sdump(rb))
		err = client.CreateCluster(gc.Project, region, request.Zone, rb)
		if err == nil {
			cluster, err = client.GetCluster(gc.Project, region, request.Zone, rb.Cluster.Name)
		}
		if err != nil {
			errMsg := fmt.Sprintf("Error during cluster creation: '%v'. ", err)
			if !common.IsProw() { // Delete half created cluster if it's user created
				errMsg = fmt.Sprintf("%sDeleting cluster %q in region %q zone %q in background...\n", errMsg, clusterName, region, request.Zone)
				client.DeleteClusterAsync(gc.Project, region, request.Zone, clusterName)
			}
			// Retry another region if cluster creation failed.
			if i != len(regions)-1 && needsRetryCreation(err.Error()) {
				errMsg = fmt.Sprintf("%sRetry another region %q for cluster creation", errMsg, regions[i+1])
			}
			log.Print(errMsg)
		} else {
			log.Print("Cluster creation completed")
			gc.Cluster = cluster
			break
		}
	}

	return err
}

// needsRetryCreation determines if cluster creation needs to be retried based on the error message.
func needsRetryCreation(errMsg string) bool {
	for _, regx := range retryableCreationErrors {
		if regx.MatchString(errMsg) {
			return true
		}
	}
	return false
}

// Delete takes care of GKE cluster resource cleanup.
// It also releases Boskos resource if running in Prow.
func (gc *GKECluster) Delete() error {
	var err error
	if err = gc.checkEnvironment(); err != nil {
		return fmt.Errorf("failed checking project/cluster from environment: '%w'", err)
	}
	gc.ensureProtected()

	// Should only get here if running locally and cluster created by this
	// client, so at this moment cluster should have been set
	if gc.Cluster == nil {
		return errors.New("cluster doesn't exist")
	}

	log.Printf("Deleting cluster %q in %q", gc.Cluster.Name, gc.Cluster.Location)
	client, err := gc.newGKEClient(gc.Project)
	if err != nil {
		return fmt.Errorf("failed creating the GKE client: '%w'", err)
	}
	region, zone := gke.RegionZoneFromLoc(gc.Cluster.Location)
	if gc.asyncCleanup {
		_, err = client.DeleteClusterAsync(gc.Project, region, zone, gc.Cluster.Name)
	} else {
		err = client.DeleteCluster(gc.Project, region, zone, gc.Cluster.Name)
	}
	if err != nil {
		return fmt.Errorf("failed deleting cluster: '%w'", err)
	}

	// Release Boskos if running in Prow
	if gc.isBoskos {
		log.Printf("Releasing Boskos resource: '%v'", gc.Project)
		if err = gc.boskosOps.ReleaseGKEProject(gc.Project); err != nil {
			return fmt.Errorf("failed releasing boskos resource: '%w'", err)
		}
	}

	return nil
}

// ensureProtected ensures not operating on protected project/cluster
func (gc *GKECluster) ensureProtected() {
	if gc.Project != "" {
		for _, pp := range protectedProjects {
			if gc.Project == pp {
				log.Fatalf("project %q is protected", gc.Project)
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

// checkEnvironment checks environment set for kubeconfig and gcloud, and try to
// identify existing project/cluster if they are not set
//
// checks for existing cluster by looking at kubeconfig, if kubeconfig is set:
// 	- If it exists in GKE:
//		- If Request doesn't contain project/clustername:
//			- Use it
//		- If Request contains any of project/clustername:
//			- If the cluster matches with them:
//				- Use it
// If cluster isn't discovered above, try to get project from gcloud
func (gc *GKECluster) checkEnvironment() error {
	output, err := common.StandardExec("kubectl", "config", "current-context")
	// if kubeconfig is configured, try to use it
	if err == nil {
		currentContext := strings.TrimSpace(string(output))
		log.Printf("kubeconfig is: %q", currentContext)
		if strings.HasPrefix(currentContext, "gke_") {
			// output should be in the form of gke_PROJECT_REGION_CLUSTER
			parts := strings.Split(currentContext, "_")
			if len(parts) != 4 { // fall through with warning
				log.Printf("WARNING: ignoring kubectl current-context since it's malformed: %q", currentContext)
			} else {
				project := parts[1]
				location, clusterName := parts[2], parts[3]
				region, zone := gke.RegionZoneFromLoc(location)
				// Use the cluster only if project and clustername match
				if (gc.Request.Project == "" || gc.Request.Project == project) && (gc.Request.ClusterName == "" || gc.Request.ClusterName == clusterName) {
					client, err := gc.newGKEClient(project)
					if err != nil {
						return fmt.Errorf("failed creating the GKE client: '%v'", err)
					}
					cluster, err := client.GetCluster(project, region, zone, clusterName)
					if err != nil {
						return fmt.Errorf("couldn't find cluster %s in %s in %s, does it exist? %w", clusterName, project, location, err)
					}
					gc.Cluster = cluster
					gc.Project = project
				}
				return nil
			}
		}
	}
	// When kubeconfig isn't set, the err isn't nil and output should be empty.
	// If output isn't empty then this is unexpected error, should shout out
	// directly
	if err != nil && len(output) > 0 {
		return fmt.Errorf("failed running kubectl config current-context: %q", string(output))
	}

	if gc.Project != "" {
		return nil
	}

	// if gcloud is pointing to a project, use it
	output, err = common.StandardExec("gcloud", "config", "get-value", "project")
	if err != nil {
		return fmt.Errorf("failed getting gcloud project: %w", err)
	}
	if os := string(output); os != "" {
		gc.Project = strings.TrimSpace(os)
	}
	return nil
}

// newGKEClient returns a new GKE client. project and environment must be provided.
func (gc *GKECluster) newGKEClient(project string) (gke.SDKOperations, error) {
	// HACK: this is merely used for unit tests.
	// Return the operation directly if it's already initialize.
	if gc.operations != nil {
		return gc.operations, nil
	}

	return gke.NewSDKClient(
		option.WithQuotaProject(project))
}
