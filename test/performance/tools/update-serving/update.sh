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

source $(dirname ${BASH_SOURCE})/../common.sh

# set up the credential for cluster operations
setup_user

# Checkout the latest serving config
get_serving

# Get all clusters to update and ko apply config. Use newline to split
header "Update all clusters"
IFS=$'\n'
for cluster in $(gcloud container clusters list --project=knative-performance --format="csv[no-heading](name,zone)"); do  
  name=$(echo $cluster | cut -f1 -d",")
  zone=$(echo $cluster | cut -f2 -d",")
  if [ ${name} = ${MASTER_CLUSTER_NAME} ]; then
    continue
  fi

  update_cluster ${name} ${zone}
done

header "Done updating all clusters"
