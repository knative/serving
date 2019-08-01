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

# Setup env vars
export PROJECT_NAME="knative-performance"
export MASTER_CLUSTER_NAME="update-serving"
export MASTER_CLUSTER_ZONE="us-west1"
export USER_NAME="mako-job@knative-performance.iam.gserviceaccount.com"
export PROJ_ROOT_PATH="$GOPATH/src/source.cloud.google.com/knative-performance/performance"
export CONTEXT_PREFIX="gke_${PROJECT_NAME}"
export SOURCE_CONTEXT="${CONTEXT_PREFIX}_${MASTER_CLUSTER_ZONE}_${MASTER_CLUSTER_NAME}"

function header() {
  echo "***** $1 *****"
}

# Exit test, dumping current state info.
# Parameters: $1 - error message (optional).
function fail_test() {  
  [[ -n $1 ]] && echo "SCRIPT ERROR: $1"
  exit 1
}

# Creates a new cluster.
# $1 -> name, $2 -> zone/region, $3 -> num_nodes
function create_cluster() {
  header "Creating cluster $1 with $3 nodes in $2"
  gcloud beta container clusters create ${1} \
    --addons=HorizontalPodAutoscaling,HttpLoadBalancing \
    --machine-type=n1-standard-4 \
    --cluster-version=latest --region=${2} \
    --enable-stackdriver-kubernetes --enable-ip-alias \
    --num-nodes=${3} \
    --enable-autorepair \
    --scopes cloud-platform
}

# Copies serice account secret to the destination
# $1 -> destination context. Should be of the form ${CONTEXT_PREFIX}_${zone}_${name}
function copy_secret() {
  gcloud container clusters get-credentials ${MASTER_CLUSTER_NAME} --project=${PROJECT_NAME} --zone=${MASTER_CLUSTER_ZONE}
  kubectl get secret service-account --context ${SOURCE_CONTEXT} --export -o yaml \
    | kubectl apply --context $1 -f -
}

# Set up the user credentials for cluster operations.
function setup_user() {
  header "Setup User"

  gcloud config set core/account ${USER_NAME}
  gcloud auth activate-service-account ${USER_NAME} --key-file=${GOOGLE_APPLICATION_CREDENTIALS}
  gcloud config set core/project ${PROJECT_NAME}

  echo "gcloud user is $(gcloud config get-value core/account)"
  echo "Using secret defined in $GOOGLE_APPLICATION_CREDENTIALS"
}

# Create a new cluster and install serving components and apply benchmark yamls
# $1 -> cluster_name, $2 -> cluster_zone, $3 -> node_count
function create_new_cluster() {
  # create a new cluster
  create_cluster $1 $2 $3
  
  # copy the secret to the new cluster
  copy_secret "${CONTEXT_PREFIX}_${2}_${1}"

  # update components on the cluster, e.g. serving and istio
  update_cluster $1 $2
}

# Get serving code and apply the patch to it.
function get_and_patch_serving() {
  header "Apply hpa patch"
  pushd .
  mkdir -p ${GOPATH}/src/knative.dev
  cd ${GOPATH}/src/knative.dev
  git clone https://github.com/knative/serving.git

  cd serving
  echo "Using ko version $(ko version)"
  echo "Patching autoscaler HPA replica count to 10"
  git apply ${PROJ_ROOT_PATH}/tools/hpa_replicaCount.patch || fail_test "Failed to apply patch"
  cd ..
  popd
}

# Update resources installed on the cluster with the up-to-date code.
# $1: name, $2: zone
function update_cluster() {
  name=$1
  zone=$2
  echo "Updating cluster with name ${name} in zone ${zone}"
  gcloud container clusters get-credentials ${name} --zone=${zone} --project knative-performance || fail_test "Failed to get cluster creds"
  
  echo ">> Delete all existing jobs and test resources"
  ko delete -f "${PROJ_ROOT_PATH}/${TEST_DIR}/$1"
  kubectl delete job --all

  pushd .
  cd ${GOPATH}/src/knative.dev
  echo ">> Update istio"
  kubectl apply -f serving/third_party/istio-1.2-latest/istio-crds.yaml || fail_test "Failed to apply istio-crds"
  kubectl apply -f serving/third_party/istio-1.2-latest/istio-lean.yaml || fail_test "Failed to apply istio-lean"

  # Overprovision the Istio gateways.
  kubectl patch hpa -n istio-system istio-ingressgateway \
        --patch '{"spec": {"minReplicas": 10, "maxReplicas": 10}}'
  kubectl patch deploy -n istio-system cluster-local-gateway \
        --patch '{"spec": {"replicas": 10}}'

  echo ">> Updating serving"
  # Retry installation for at most two times as there can sometime be a race condition when applying serving CRDs
  local n=0
  until [ $n -ge 2 ]
  do
    ko apply -f serving/config/ -f serving/config/v1beta1 && break
    n=$[$n+1]
  done
  if [ $n == 2 ]; then
    fail_test "Failed to patch serving"
  fi
  popd

  echo ">> Applying all the yamls"
  # install the service and cronjob to run the benchmark
  # NOTE: this assumes we have a benchmark with the same name as the cluster
  # If service creation takes long time, we will have some intially unreachable errors in the test
  cd $PROJ_ROOT_PATH
  ko apply -f ${TEST_DIR}/$1 || fail_test "Failed to apply benchmarks yaml"
}
