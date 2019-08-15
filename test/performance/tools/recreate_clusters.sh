#!/bin/bash

# Copyright 2019 The Knative Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

source $(dirname ${BASH_SOURCE})/common.sh

# set up the user credentials for cluster operations
setup_user

header "Recreating all clusters"
for cluster in $(gcloud container clusters list --project="${PROJECT_NAME}" --format="csv[no-heading](name,zone,currentNodeCount)"); do
  name=$(echo $cluster | cut -f1 -d",")
  zone=$(echo $cluster | cut -f2 -d",")
  node_count=$(echo $cluster | cut -f3 -d",")
  (( node_count=node_count/3 ))

  # delete the old cluster
  gcloud container clusters delete ${name} --zone ${zone} --quiet

  # create a new cluster and update all the components
  create_new_cluster ${name} ${zone} ${node_count}
done

header "Done recreating all clusters"
