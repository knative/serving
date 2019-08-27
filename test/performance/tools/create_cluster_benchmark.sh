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

source $(dirname ${BASH_SOURCE})/common.sh

function parse_flags() {
    case $1 in
        --num_nodes) NUM_NODES=$2 ;;
        --name) CLUSTER_NAME=$2 ;;
        --region) CLUSTER_REGION=$2 ;;
    esac
    return 0
}

while [[ $# -ne 0 ]]; do
    parse_flags $@    
    shift
    shift
done

[[ ! -z "$CLUSTER_NAME" ]] || abort "Cluster name not set"
[[ ! -z "$NUM_NODES" ]] || abort "Number of nodes not set"
[[ ! -z "$CLUSTER_REGION" ]] || abort "Cluster region not set"

create_new_cluster $CLUSTER_NAME $CLUSTER_REGION $NUM_NODES

# Unset the service account config
kubectl config unset current-context
