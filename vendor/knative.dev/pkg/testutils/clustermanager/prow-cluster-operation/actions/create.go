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

package actions

import (
	"fmt"
	"log"
	"strconv"

	container "google.golang.org/api/container/v1beta1"
	"knative.dev/pkg/test/gke"
	clm "knative.dev/pkg/testutils/clustermanager/e2e-tests"
	"knative.dev/pkg/testutils/clustermanager/e2e-tests/common"
	"knative.dev/pkg/testutils/clustermanager/prow-cluster-operation/options"
	"knative.dev/pkg/testutils/metahelper/client"
)

const (
	// Keys to be written into metadata.json
	e2eRegionKey      = "E2E:Region"
	e2eZoneKey        = "E2E:Zone"
	clusterNameKey    = "E2E:Machine"
	clusterVersionKey = "E2E:Version"
	minNodesKey       = "E2E:MinNodes"
	maxNodesKey       = "E2E:MaxNodes"
	projectKey        = "E2E:Project"
)

// writeMetadata writes the cluster information to a metadata file defined in the request
// after the cluster operation is finished
func writeMetaData(cluster *container.Cluster, project string) {
	// Set up metadata client for saving metadata
	c, err := client.NewClient("")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Writing metadata to: %q", c.Path)
	// Get minNodes and maxNodes counts from default-pool, this is
	// usually the case in tests in Prow
	var minNodes, maxNodes string
	for _, np := range cluster.NodePools {
		if np.Name == "default-pool" {
			minNodes = strconv.FormatInt(np.InitialNodeCount, 10)
			// maxNodes is equal to minNodes if autoscaling isn't on
			maxNodes = minNodes
			if np.Autoscaling != nil {
				minNodes = strconv.FormatInt(np.Autoscaling.MinNodeCount, 10)
				maxNodes = strconv.FormatInt(np.Autoscaling.MaxNodeCount, 10)
			} else {
				log.Printf("DEBUG: nodepool is default-pool but autoscaling is not on: '%+v'", np)
			}
			break
		}
	}

	e2eRegion, e2eZone := gke.RegionZoneFromLoc(cluster.Location)
	for key, val := range map[string]string{
		e2eRegionKey:      e2eRegion,
		e2eZoneKey:        e2eZone,
		clusterNameKey:    cluster.Name,
		clusterVersionKey: cluster.InitialClusterVersion,
		minNodesKey:       minNodes,
		maxNodesKey:       maxNodes,
		projectKey:        project,
	} {
		if err = c.Set(key, val); err != nil {
			log.Fatalf("Failed saving metadata %q:%q: '%v'", key, val, err)
		}
	}
	log.Println("Done writing metadata")
}

// Create creates a GKE cluster and configures gcloud after successful GKE create request
func Create(o *options.RequestWrapper) (*clm.GKECluster, error) {
	o.Prep()

	gkeClient := clm.GKEClient{}
	clusterOps := gkeClient.Setup(o.Request)
	gkeOps := clusterOps.(*clm.GKECluster)
	if err := gkeOps.Acquire(); err != nil || gkeOps.Cluster == nil {
		return nil, fmt.Errorf("failed acquiring GKE cluster: '%v'", err)
	}

	// At this point we should have a cluster ready to run test. Need to save
	// metadata so that following flow can understand the context of cluster, as
	// well as for Prow usage later
	writeMetaData(gkeOps.Cluster, gkeOps.Project)

	// set up kube config points to cluster
	// TODO(chaodaiG): this probably should also be part of clustermanager lib
	if out, err := common.StandardExec("gcloud", "beta", "container", "clusters", "get-credentials",
		gkeOps.Cluster.Name, "--region", gkeOps.Cluster.Location, "--project", gkeOps.Project); err != nil {
		return nil, fmt.Errorf("failed connecting to cluster: %q, '%v'", out, err)
	}
	if out, err := common.StandardExec("gcloud", "config", "set", "project", gkeOps.Project); err != nil {
		return nil, fmt.Errorf("failed setting gcloud: %q, '%v'", out, err)
	}

	return gkeOps, nil
}
