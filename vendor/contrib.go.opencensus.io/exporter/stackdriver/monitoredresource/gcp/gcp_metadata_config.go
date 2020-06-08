// Copyright 2020, OpenCensus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gcp

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/compute/metadata"
	container "cloud.google.com/go/container/apiv1"
	containerpb "google.golang.org/genproto/googleapis/container/v1"
)

// gcpMetadata represents metadata retrieved from GCP (GKE and GCE) environment.
type gcpMetadata struct {

	// projectID is the identifier of the GCP project associated with this resource, such as "my-project".
	projectID string

	// instanceID is the numeric VM instance identifier assigned by Compute Engine.
	instanceID string

	// clusterName is the name for the cluster the container is running in.
	clusterName string

	// containerName is the name of the container.
	containerName string

	// namespaceID is the identifier for the cluster namespace the container is running in
	namespaceID string

	// podID is the identifier for the pod the container is running in.
	podID string

	// zone is the Compute Engine zone in which the VM is running.
	zone string

	monitoringV2 bool
}

// retrieveGCPMetadata retrieves value of each Attribute from Metadata Server
// in GKE container and GCE instance environment.
// Some attributes are retrieved from the system environment.
// This is only executed detectOnce.
func retrieveGCPMetadata() *gcpMetadata {
	gcpMetadata := gcpMetadata{}
	var err error
	gcpMetadata.instanceID, err = metadata.InstanceID()
	if err != nil {
		// Not a GCP environment
		return &gcpMetadata
	}

	gcpMetadata.projectID, err = metadata.ProjectID()
	logError(err)

	gcpMetadata.zone, err = metadata.Zone()
	logError(err)

	clusterName, err := metadata.InstanceAttributeValue("cluster-name")
	logError(err)
	gcpMetadata.clusterName = strings.TrimSpace(clusterName)

	clusterLocation, err := metadata.InstanceAttributeValue("cluster-location")
	logError(err)

	// Following attributes are derived from environment variables. They are configured
	// via yaml file. For details refer to:
	// https://cloud.google.com/kubernetes-engine/docs/tutorials/custom-metrics-autoscaling#exporting_metrics_from_the_application
	gcpMetadata.namespaceID = os.Getenv("NAMESPACE")
	gcpMetadata.containerName = os.Getenv("CONTAINER_NAME")
	gcpMetadata.podID = os.Getenv("HOSTNAME")

	// Monitoring API version can be obtained from cluster info.q
	if gcpMetadata.clusterName != "" {
		ctx := context.Background()
		c, err := container.NewClusterManagerClient(ctx)
		logError(err)
		if c != nil {
			req := &containerpb.GetClusterRequest{
				Name: fmt.Sprintf("projects/%s/locations/%s/clusters/%s", gcpMetadata.projectID, strings.TrimSpace(clusterLocation), gcpMetadata.clusterName),
			}
			resp, err := c.GetCluster(ctx, req)
			logError(err)
			if resp != nil && resp.GetMonitoringService() == "monitoring.googleapis.com/kubernetes" &&
				resp.GetLoggingService() == "logging.googleapis.com/kubernetes" {
				gcpMetadata.monitoringV2 = true
			}
		}
	}

	return &gcpMetadata
}

// logError logs error only if the error is present and it is not 'not defined'
func logError(err error) {
	if err != nil {
		if !strings.Contains(err.Error(), "not defined") {
			log.Printf("Error retrieving gcp metadata: %v", err)
		}
	}
}
