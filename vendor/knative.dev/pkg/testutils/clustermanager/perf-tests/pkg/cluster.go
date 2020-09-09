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

package pkg

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"sync"

	"google.golang.org/api/option"

	"knative.dev/pkg/test/gke"
	"knative.dev/pkg/test/helpers"

	container "google.golang.org/api/container/v1beta1"
)

const (
	// the maximum retry times if there is an error in cluster operation
	retryTimes = 3

	// known cluster status
	// TODO(chizhg): move these status constants to gke package
	statusProvisioning = "PROVISIONING"
	statusStopping     = "STOPPING"
)

// Extra configurations we want to support for cluster creation request.
var (
	enableWorkloadIdentity = flag.Bool("enable-workload-identity", false, "whether to enable Workload Identity")
	serviceAccount         = flag.String("service-account", "", "service account that will be used on this cluster")
)

type gkeClient struct {
	ops gke.SDKOperations
}

// NewClient will create a new gkeClient.
func NewClient(environment string) (*gkeClient, error) {
	endpoint, err := gke.ServiceEndpoint(environment)
	if err != nil {
		return nil, err
	}
	endpointOption := option.WithEndpoint(endpoint)
	operations, err := gke.NewSDKClient(endpointOption)
	if err != nil {
		return nil, fmt.Errorf("failed to set up GKE client: %w", err)
	}

	client := &gkeClient{
		ops: operations,
	}
	return client, nil
}

// RecreateClusters will delete and recreate the existing clusters, it will also create the clusters if they do
// not exist for the corresponding benchmarks.
func (gc *gkeClient) RecreateClusters(gcpProject, repo, benchmarkRoot string) error {
	handleExistingCluster := func(cluster container.Cluster, configExists bool, config ClusterConfig) error {
		// always delete the cluster, even if the cluster config is unchanged
		return gc.handleExistingClusterHelper(gcpProject, cluster, configExists, config, false)
	}
	handleNewClusterConfig := func(clusterName string, clusterConfig ClusterConfig) error {
		// create a new cluster with the new cluster config
		return gc.createClusterWithRetries(gcpProject, clusterName, clusterConfig)
	}
	return gc.processClusters(gcpProject, repo, benchmarkRoot, handleExistingCluster, handleNewClusterConfig)
}

// ReconcileClusters will reconcile all clusters to make them consistent with the benchmarks' cluster configs.
//
// There can be 4 scenarios:
// 1. If the benchmark's cluster config is unchanged, do nothing
// 2. If the benchmark's config is changed, delete the old cluster and create a new one with the new config
// 3. If the benchmark is renamed, delete the old cluster and create a new one with the new name
// 4. If the benchmark is deleted, delete the corresponding cluster
func (gc *gkeClient) ReconcileClusters(gcpProject, repo, benchmarkRoot string) error {
	handleExistingCluster := func(cluster container.Cluster, configExists bool, config ClusterConfig) error {
		// retain the cluster, if the cluster config is unchanged
		return gc.handleExistingClusterHelper(gcpProject, cluster, configExists, config, true)
	}
	handleNewClusterConfig := func(clusterName string, clusterConfig ClusterConfig) error {
		// create a new cluster with the new cluster config
		return gc.createClusterWithRetries(gcpProject, clusterName, clusterConfig)
	}
	return gc.processClusters(gcpProject, repo, benchmarkRoot, handleExistingCluster, handleNewClusterConfig)
}

// DeleteClusters will delete all existing clusters.
func (gc *gkeClient) DeleteClusters(gcpProject, repo, benchmarkRoot string) error {
	handleExistingCluster := func(cluster container.Cluster, configExists bool, config ClusterConfig) error {
		// retain the cluster, if the cluster config is unchanged
		return gc.deleteClusterWithRetries(gcpProject, cluster)
	}
	handleNewClusterConfig := func(clusterName string, clusterConfig ClusterConfig) error {
		// do nothing
		return nil
	}
	return gc.processClusters(gcpProject, repo, benchmarkRoot, handleExistingCluster, handleNewClusterConfig)
}

// processClusters will process existing clusters and configs for new clusters,
// with the corresponding functions provided by callers.
func (gc *gkeClient) processClusters(
	gcpProject, repo, benchmarkRoot string,
	handleExistingCluster func(cluster container.Cluster, configExists bool, config ClusterConfig) error,
	handleNewClusterConfig func(name string, config ClusterConfig) error,
) error {
	curtClusters, err := gc.listClustersForRepo(gcpProject, repo)
	if err != nil {
		return fmt.Errorf("failed getting clusters for the repo %q: %w", repo, err)
	}
	clusterConfigs, err := benchmarkClusters(repo, benchmarkRoot)
	if err != nil {
		return fmt.Errorf("failed getting cluster configs for benchmarks in repo %q: %w", repo, err)
	}

	errCh := make(chan error, len(curtClusters)+len(clusterConfigs))
	wg := sync.WaitGroup{}
	// handle all existing clusters
	for i := range curtClusters {
		wg.Add(1)
		cluster := curtClusters[i]
		config, configExists := clusterConfigs[cluster.Name]
		go func() {
			defer wg.Done()
			if err := handleExistingCluster(cluster, configExists, config); err != nil {
				errCh <- fmt.Errorf("failed handling cluster %v: %w", cluster, err)
			}
		}()
		// remove the cluster from clusterConfigs as it's already been handled
		delete(clusterConfigs, cluster.Name)
	}

	// handle all other cluster configs
	for name, config := range clusterConfigs {
		wg.Add(1)
		// recreate them to avoid the issue with iterations of multiple Go routines
		name, config := name, config
		go func() {
			defer wg.Done()
			if err := handleNewClusterConfig(name, config); err != nil {
				errCh <- fmt.Errorf("failed handling new cluster config %v: %w", config, err)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	errs := make([]error, len(errCh))
	for err := range errCh {
		errs = append(errs, err)
	}

	return helpers.CombineErrors(errs)
}

// handleExistingClusterHelper is a helper function for handling an existing cluster.
func (gc *gkeClient) handleExistingClusterHelper(
	gcpProject string,
	cluster container.Cluster, configExists bool, config ClusterConfig,
	retainIfUnchanged bool,
) error {
	// if the cluster is currently being created or deleted, return directly as that job will handle it properly
	if cluster.Status == statusProvisioning || cluster.Status == statusStopping {
		log.Printf("Cluster %q is being handled by another job, skip it", cluster.Name)
		return nil
	}

	curtNodeCount := cluster.CurrentNodeCount
	// if it's a regional cluster, the nodes will be in 3 zones. The CurrentNodeCount we get here is
	// the total node count, so we'll need to divide with 3 to get the actual regional node count
	if _, zone := gke.RegionZoneFromLoc(cluster.Location); zone == "" {
		curtNodeCount /= 3
	}
	// if retainIfUnchanged is set to true, and the cluster config does not change, do nothing
	// TODO(chizhg): also check the addons config
	if configExists && retainIfUnchanged &&
		curtNodeCount == config.NodeCount && cluster.Location == config.Location {
		log.Printf("Cluster config is unchanged for %q, skip it", cluster.Name)
		return nil
	}

	if err := gc.deleteClusterWithRetries(gcpProject, cluster); err != nil {
		return fmt.Errorf("failed deleting cluster %q in %q: %w", cluster.Name, cluster.Location, err)
	}
	if configExists {
		return gc.createClusterWithRetries(gcpProject, cluster.Name, config)
	}
	return nil
}

// listClustersForRepo will list all the clusters under the gcpProject that belong to the given repo.
func (gc *gkeClient) listClustersForRepo(gcpProject, repo string) ([]container.Cluster, error) {
	allClusters, err := gc.ops.ListClustersInProject(gcpProject)
	if err != nil {
		return nil, fmt.Errorf("failed listing clusters in project %q: %w", gcpProject, err)
	}

	clusters := make([]container.Cluster, 0)
	for _, cluster := range allClusters {
		if clusterBelongsToRepo(cluster.Name, repo) {
			clusters = append(clusters, *cluster)
		}
	}
	return clusters, nil
}

// deleteClusterWithRetries will delete the given cluster,
// and retry for a maximum of retryTimes if there is an error.
// TODO(chizhg): maybe move it to clustermanager library.
func (gc *gkeClient) deleteClusterWithRetries(gcpProject string, cluster container.Cluster) error {
	log.Printf("Deleting cluster %q under project %q", cluster.Name, gcpProject)
	region, zone := gke.RegionZoneFromLoc(cluster.Location)
	var err error
	for i := 0; i < retryTimes; i++ {
		if err = gc.ops.DeleteCluster(gcpProject, region, zone, cluster.Name); err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Errorf(
			"failed deleting cluster %q in %q after retrying %d times: %w",
			cluster.Name, cluster.Location, retryTimes, err)
	}

	return nil
}

// createClusterWithRetries will create a new cluster with the given config,
// and retry for a maximum of retryTimes if there is an error.
// TODO(chizhg): maybe move it to clustermanager library.
func (gc *gkeClient) createClusterWithRetries(gcpProject, name string, config ClusterConfig) error {
	log.Printf("Creating cluster %q under project %q with config %v", name, gcpProject, config)
	var addons []string
	if strings.TrimSpace(config.Addons) != "" {
		addons = strings.Split(config.Addons, ",")
	}
	req := &gke.Request{
		Project:                gcpProject,
		ClusterName:            name,
		MinNodes:               config.NodeCount,
		MaxNodes:               config.NodeCount,
		NodeType:               config.NodeType,
		Addons:                 addons,
		EnableWorkloadIdentity: *enableWorkloadIdentity,
		ServiceAccount:         *serviceAccount,
	}
	creq, err := gke.NewCreateClusterRequest(req)
	if err != nil {
		return fmt.Errorf("cannot create cluster with request %v: %w", req, err)
	}

	region, zone := gke.RegionZoneFromLoc(config.Location)
	for i := 0; i < retryTimes; i++ {
		// TODO(chizhg): retry with different requests, based on the error type
		if err = gc.ops.CreateCluster(gcpProject, region, zone, creq); err != nil {
			// If the cluster is actually created in the end, recreating it with the same name will fail again for sure,
			// so we need to delete the broken cluster before retry.
			// It is a best-effort delete, and won't throw any errors if the deletion fails.
			if cluster, _ := gc.ops.GetCluster(gcpProject, region, zone, name); cluster != nil {
				gc.deleteClusterWithRetries(gcpProject, *cluster)
			}
		} else {
			break
		}
	}
	if err != nil {
		return fmt.Errorf(
			"failed creating cluster %q in %q after retrying %d times: %w",
			name, config.Location, retryTimes, err)
	}

	return nil
}
