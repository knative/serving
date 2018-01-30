#!/usr/bin/env bash

# Copyright 2018 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

echo "Running one-time setup for project ${PROJECT_ID?'need export PROJECT_ID='}"

echo "Enabling GKE"
gcloud --project=$PROJECT_ID service-management enable container.googleapis.com

echo "Creating cluster"
gcloud --project=$PROJECT_ID container clusters create \
  --zone=us-central1-a \
  $PROJECT_ID-cluster

echo "Deleting the default node pool"
# This is necessary since GKE clusters use the GCE service account for application default credentials,
# while Flex expects to be running as the appspot service account
#
gcloud --quiet --project=$PROJECT_ID container node-pools delete \
  --cluster=$PROJECT_ID-cluster --zone=us-central1-a \
  default-pool

# needed to create the service account used in the next step
gcloud app create --region=us-central

echo
echo "Please create a new node pool using API Explorer: https://developers.google.com/apis-explorer/#search/container/container/v1/container.projects.zones.clusters.nodePools.create"
echo
echo "projectId: $PROJECT_ID"
echo "zone: us-central1-a"
echo "clusterId: $PROJECT_ID-cluster"
echo "Request body:"
echo "{
  \"nodePool\": {
    \"initialNodeCount\": 5,
    \"name\": \"app-pool\",
    \"config\": {
      \"machineType\": \"n1-standard-1\",
      \"diskSizeGb\": 100,
      \"oauthScopes\": [
        \"https://www.googleapis.com/auth/appengine.apis\",
        \"https://www.googleapis.com/auth/cloud-platform\",
        \"https://www.googleapis.com/auth/devstorage.full_control\",
        \"https://www.googleapis.com/auth/logging.write\",
        \"https://www.googleapis.com/auth/userinfo.email\"
      ],
      \"imageType\": \"COS\",
      \"serviceAccount\": \"$PROJECT_ID@appspot.gserviceaccount.com\"
    }
  }
}
"
echo "Press any key when node pool creation is finished..."
read -n 1

$(dirname $0)/cluster-setup.sh
