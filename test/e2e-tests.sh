#!/bin/bash

# Copyright 2018 Google, Inc. All rights reserved.
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

# This script runs the end-to-end tests against Elafros built from source.
# It is started by prow for each PR.
# For convenience, it can also be executed manually.

# If you already have the *_OVERRIDE environment variables set, call
# this script with the --run-tests arguments and it will start elafros in
# the cluster and run the tests.

# Calling this script without arguments will create a new cluster in
# project $PROJECT_ID, start elafros in it, run the tests and delete the
# cluster. $DOCKER_REPO_OVERRIDE must point to a valid writable docker repo.

# Test cluster parameters and location of generated test images
readonly E2E_CLUSTER_ZONE=us-east1-d
readonly E2E_CLUSTER_NODES=2
readonly E2E_CLUSTER_MACHINE=n1-standard-2
readonly GKE_VERSION=v1.9.4-gke.1
readonly E2E_DOCKER_BASE=gcr.io/elafros-e2e-tests

# Unique identifier for this test execution
# uuidgen is not available in kubekins images
readonly UUID=$(cat /proc/sys/kernel/random/uuid)

# Useful environment variables
[[ $USER == "prow" ]] && IS_PROW=1 || IS_PROW=0
readonly IS_PROW
readonly SCRIPT_CANONICAL_PATH="$(readlink -f ${BASH_SOURCE})"
readonly SCRIPT_ROOT="$(dirname ${SCRIPT_CANONICAL_PATH})/.."

# Save *_OVERRIDE variables in case a bazel cleanup if required.
readonly OG_DOCKER_REPO="${DOCKER_REPO_OVERRIDE}"
readonly OG_K8S_CLUSTER="${K8S_CLUSTER_OVERRIDE}"
readonly OG_K8S_USER="${K8S_USER_OVERRIDE}"

function header() {
  echo "================================================="
  echo $1
  echo "================================================="
}

function cleanup_bazel() {
  header "Cleaning up Bazel"
  export DOCKER_REPO_OVERRIDE="${OG_DOCKER_REPO}"
  export K8S_CLUSTER_OVERRIDE="${OG_K8S_CLUSTER}"
  export K8S_USER_OVERRIDE="${OG_K8S_CLUSTER}"
  # --expunge is a workaround for https://github.com/elafros/elafros/issues/366
  bazel clean --expunge
}

function teardown() {
  header "Tearing down test environment"
  # Free resources in GCP project.
  if (( ! USING_EXISTING_CLUSTER )); then
    bazel run //:everything.delete
  fi

  # Delete Elafros images when using prow.
  if (( IS_PROW )); then
    delete_elafros_images
  else
    cleanup_bazel
  fi
}

function wait_for_elafros() {
  echo -n "Waiting for Elafros to come up"
  for i in {1..150}; do  # timeout after 5 minutes
    if [[ $(kubectl -n ela-system get pods | grep "Running" | wc -l) == 2 ]]; then
      echo -e "\nElafros is up"
      return 0
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: timeout waiting for Elafros to come up"
  return 1
}

function delete_elafros_images() {
  local all_images=""
  for image in build-controller creds-image ela-autoscaler ela-controller ela-queue ela-webhook git-image ; do
    all_images="${all_images} ${ELA_DOCKER_REPO}/${image}"
  done
  gcloud -q container images delete ${all_images}
}

function run_conformance_tests() {
  header "Running conformance tests"
  echo -e "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: pizzaplanet" | kubectl create -f -
  go test -v ./test/conformance -ginkgo.v -dockerrepo ${E2E_DOCKER_BASE}/ela-conformance-test
}

# Script entry point.

cd ${SCRIPT_ROOT}

# Show help if bad arguments are passed.
if [[ -n $1 && $1 != "--run-tests" ]]; then
  echo "usage: $0 [--run-tests]"
  exit 1
fi

# No argument provided, create the test cluster.

if [[ -z $1 ]]; then
  header "Creating test cluster"
  # Smallest cluster required to run the end-to-end-tests
  CLUSTER_CREATION_ARGS=(
    --gke-create-args="--enable-autoscaling --min-nodes=1 --max-nodes=${E2E_CLUSTER_NODES} --scopes=cloud-platform"
    --gke-shape={\"default\":{\"Nodes\":${E2E_CLUSTER_NODES}\,\"MachineType\":\"${E2E_CLUSTER_MACHINE}\"}}
    --provider=gke
    --deployment=gke
    --gcp-node-image=cos
    --cluster=ela-e2e-cluster
    --gcp-zone="${E2E_CLUSTER_ZONE}"
    --gcp-network=ela-e2e-net
    --gke-environment=prod
  )
  if (( ! IS_PROW )); then
    if [[ -z ${DOCKER_REPO_OVERRIDE} ]]; then
      : ${DOCKER_REPO_OVERRIDE:?"DOCKER_REPO_OVERRIDE must be set to  writeable docker repo."}
      return 1
    fi
    CLUSTER_CREATION_ARGS+=(--gcp-project=${PROJECT_ID:?"PROJECT_ID must be set to the GCP project where the tests are run."})
  fi
  # Clear user and cluster variables, so they'll be set to the test cluster.
  # DOCKER_REPO_OVERRIDE is not touched because when running locally it must
  # be a writeable docker repo.
  export K8S_USER_OVERRIDE=
  export K8S_CLUSTER_OVERRIDE=
  kubetest "${CLUSTER_CREATION_ARGS[@]}" \
    --up \
    --down \
    --extract "${GKE_VERSION}" \
    --test-cmd "${SCRIPT_CANONICAL_PATH}" \
    --test-cmd-args --run-tests
  exit
fi

# --run-tests passed as first argument, run the tests.

# Set the required variables if necessary.

if [[ -z ${K8S_USER_OVERRIDE} ]]; then
  export K8S_USER_OVERRIDE=$(gcloud config get-value core/account)
fi

USING_EXISTING_CLUSTER=1
if [[ -z ${K8S_CLUSTER_OVERRIDE} ]]; then
  USING_EXISTING_CLUSTER=0
  export K8S_CLUSTER_OVERRIDE=$(kubectl config current-context)
  # Fresh new test cluster, set cluster-admin.
  kubectl create clusterrolebinding cluster-admin-binding \
    --clusterrole=cluster-admin \
    --user=${K8S_USER_OVERRIDE}
fi
readonly USING_EXISTING_CLUSTER

if [[ -z ${DOCKER_REPO_OVERRIDE} ]]; then
  export DOCKER_REPO_OVERRIDE=${E2E_DOCKER_BASE}/ela-images-e2e-${UUID}
fi
readonly ELA_DOCKER_REPO=${DOCKER_REPO_OVERRIDE}

# Build and start Elafros.

echo "================================================="
echo "* Cluster is ${K8S_CLUSTER_OVERRIDE}"
echo "* User is ${K8S_USER_OVERRIDE}"
echo "* Docker is ${DOCKER_REPO_OVERRIDE}"

header "Building and starting Elafros"
trap teardown EXIT

# --expunge is a workaround for https://github.com/elafros/elafros/issues/366
bazel clean --expunge
if (( USING_EXISTING_CLUSTER )); then
  echo "Deleting any previous Elafros instance"
  bazel run //:everything.delete  # ignore if not running
fi

bazel run //:everything.apply
wait_for_elafros || exit 1

# Run the tests

run_conformance_tests
test_result=$?

exit ${test_result}
