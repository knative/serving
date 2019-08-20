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
export USER_NAME="mako-job@knative-performance.iam.gserviceaccount.com"
export TEST_ROOT_PATH="$GOPATH/src/knative.dev/serving/test/performance"
export KO_DOCKER_REPO="gcr.io/knative-performance"

function header() {
  echo "***** $1 *****"
}

# Exit script, dumping current state info.
# Parameters: $1 - error message (optional).
function abort() {
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

# Create serice account secret on the cluster.
# $1 -> cluster_name, $2 -> cluster_zone
function create_secret() {
  echo "Create service account on cluster $1 in zone $2"
  gcloud container clusters get-credentials $1 --zone=$2 --project=${PROJECT_NAME} || abort "Failed to get cluster creds"
  kubectl create secret generic service-account --from-file=robot.json=${PERF_TEST_GOOGLE_APPLICATION_CREDENTIALS}
}

# Set up the user credentials for cluster operations.
function setup_user() {
  header "Setup User"

  gcloud config set core/account ${USER_NAME}
  gcloud auth activate-service-account ${USER_NAME} --key-file=${PERF_TEST_GOOGLE_APPLICATION_CREDENTIALS}
  gcloud config set core/project ${PROJECT_NAME}

  echo "gcloud user is $(gcloud config get-value core/account)"
  echo "Using secret defined in ${PERF_TEST_GOOGLE_APPLICATION_CREDENTIALS}"
}

# Create a new cluster and install serving components and apply benchmark yamls.
# $1 -> cluster_name, $2 -> cluster_zone, $3 -> node_count
function create_new_cluster() {
  # create a new cluster
  create_cluster $1 $2 $3
  
  # create the secret on the new cluster
  create_secret $1 $2

  # update components on the cluster, e.g. serving and istio
  update_cluster $1 $2
}

# Update resources installed on the cluster with the up-to-date code.
# $1 -> cluster_name, $2 -> cluster_zone
function update_cluster() {
  name=$1
  zone=$2
  echo "Updating cluster with name ${name} in zone ${zone}"
  gcloud container clusters get-credentials ${name} --zone=${zone} --project=${PROJECT_NAME} || abort "Failed to get cluster creds"
  
  echo ">> Delete all existing jobs and test resources"
  kubectl delete job --all
  ko delete -f "${TEST_ROOT_PATH}/$1"

  pushd .
  cd ${GOPATH}/src/knative.dev
  echo ">> Update istio"
  kubectl apply -f serving/third_party/istio-1.2-latest/istio-crds.yaml || abort "Failed to apply istio-crds"
  kubectl apply -f serving/third_party/istio-1.2-latest/istio-lean.yaml || abort "Failed to apply istio-lean"

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
    abort "Failed to patch serving"
  fi
  popd

  # Update the activator hpa minReplicas to 10
  kubectl patch hpa -n knative-serving activator \
        --patch '{"spec": {"minReplicas": 10}}'

  echo ">> Setting up 'prod' config-mako"
  cat | kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-mako
data:
  # This should only be used by our performance automation.
  environment: prod
EOF

  echo ">> Applying all the yamls"
  # install the service and cronjob to run the benchmark
  # NOTE: this assumes we have a benchmark with the same name as the cluster
  # If service creation takes long time, we will have some intially unreachable errors in the test
  cd $TEST_ROOT_PATH
  echo "Using ko version $(ko version)"
  ko apply -f $1 || abort "Failed to apply benchmarks yaml"
}
