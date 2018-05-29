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

source "$(dirname $(readlink -f ${BASH_SOURCE}))/library.sh"

# Test cluster parameters and location of generated test images
readonly E2E_CLUSTER_NAME=ela-e2e-cluster${BUILD_NUMBER}
readonly E2E_NETWORK_NAME=ela-e2e-net${BUILD_NUMBER}
readonly E2E_CLUSTER_ZONE=us-central1-a
readonly E2E_CLUSTER_NODES=3
readonly E2E_CLUSTER_MACHINE=n1-standard-4
readonly TEST_RESULT_FILE=/tmp/ela-e2e-result
readonly ISTIO_VERSION=0.6.0
readonly ISTIO_DIR=./third_party/istio-${ISTIO_VERSION}/install/kubernetes

# This script.
readonly SCRIPT_CANONICAL_PATH="$(readlink -f ${BASH_SOURCE})"

# Helper functions.

function create_istio() {
  kubectl apply -f ${ISTIO_DIR}/istio.yaml

  ${ISTIO_DIR}/webhook-create-signed-cert.sh \
    --service istio-sidecar-injector \
    --namespace istio-system \
    --secret sidecar-injector-certs

  kubectl apply -f ${ISTIO_DIR}/istio-sidecar-injector-configmap-release.yaml

  cat ${ISTIO_DIR}/istio-sidecar-injector.yaml | \
    ${ISTIO_DIR}/webhook-patch-ca-bundle.sh | \
    kubectl apply -f -
}

function create_everything() {
  create_istio
  kubectl apply -f third_party/config/build/release.yaml
  ko apply -f config/
}

function delete_istio() {
  kubectl delete --ignore-not-found=true \
    -f ${ISTIO_DIR}/istio-sidecar-injector.yaml \
    -f ${ISTIO_DIR}/istio-sidecar-injector-configmap-release.yaml \
    -f ${ISTIO_DIR}/istio.yaml
  kubectl delete clusterrolebinding cluster-admin-binding
}

function delete_everything() {
  ko delete --ignore-not-found=true -f config/
  kubectl delete --ignore-not-found=true -f third_party/config/build/release.yaml
  delete_istio
}

function teardown() {
  header "Tearing down test environment"
  # Free resources in GCP project.
  if (( ! USING_EXISTING_CLUSTER )); then
    delete_everything
  fi

  # Delete Elafros images when using prow.
  if (( IS_PROW )); then
    echo "Images in ${DOCKER_REPO_OVERRIDE}:"
    gcloud container images list --repository=${DOCKER_REPO_OVERRIDE}
    delete_gcr_images ${DOCKER_REPO_OVERRIDE}
  else
    restore_override_vars
  fi
}

function exit_if_failed() {
  [[ $? -eq 0 ]] && return 0
  echo "***************************************"
  echo "***           TEST FAILED           ***"
  echo "***    Start of information dump    ***"
  echo "***************************************"
  if (( IS_PROW )) || [[ $PROJECT_ID != "" ]]; then
    echo ">>> Project info:"
    gcloud compute project-info describe
  fi
  echo ">>> All resources:"
  kubectl get all --all-namespaces
  echo ">>> Services:"
  kubectl get services --all-namespaces
  echo ">>> Events:"
  kubectl get events --all-namespaces
  echo ">>> Routes:"
  kubectl get routes -o yaml --all-namespaces
  echo ">>> Configurations:"
  kubectl get configurations -o yaml --all-namespaces
  echo ">>> Revisions:"
  kubectl get revisions -o yaml --all-namespaces
  echo ">>> Ingress:"
  kubectl get ingress --all-namespaces
  echo ">>> Elafros controller log:"
  kubectl logs $(get_ela_pod ela-controller) -n ela-system
  echo "***************************************"
  echo "***           TEST FAILED           ***"
  echo "***     End of information dump     ***"
  echo "***************************************"
  exit 1
}

function run_tests() {
  header "Running tests in $1"
  kubectl create namespace $2
  local started=$(date +%s)
  go test -v ./test/$1 -dockerrepo gcr.io/elafros-e2e-tests/$3
  local result=$?
  local ended=$(date +%s)
  local elapsed=$(( ended - started ))
  local status
  [[ $result -eq 0 ]] && status="PASSED" || status="FAILED"
  echo "//test/$1:all                    ${status} in ${elapsed}s"
  [[ $result -eq 0 ]]
  exit_if_failed
}

# Script entry point.

cd ${ELAFROS_ROOT_DIR}

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
    --cluster="${E2E_CLUSTER_NAME}"
    --gcp-zone="${E2E_CLUSTER_ZONE}"
    --gcp-network="${E2E_NETWORK_NAME}"
    --gke-environment=prod
  )
  if (( ! IS_PROW )); then
    CLUSTER_CREATION_ARGS+=(--gcp-project=${PROJECT_ID:?"PROJECT_ID must be set to the GCP project where the tests are run."})
  else
    # On prow, set bogus SSH keys for kubetest, we're not using them.
    touch $HOME/.ssh/google_compute_engine.pub
    touch $HOME/.ssh/google_compute_engine
  fi
  # Clear user and cluster variables, so they'll be set to the test cluster.
  # DOCKER_REPO_OVERRIDE is not touched because when running locally it must
  # be a writeable docker repo.
  export K8S_USER_OVERRIDE=
  export K8S_CLUSTER_OVERRIDE=
  # Assume test failed (see more details at the end of this script).
  echo -n "1"> ${TEST_RESULT_FILE}
  kubetest "${CLUSTER_CREATION_ARGS[@]}" \
    --up \
    --down \
    --extract "v${ELAFROS_GKE_VERSION}" \
    --test-cmd "${SCRIPT_CANONICAL_PATH}" \
    --test-cmd-args --run-tests
  result="$(cat ${TEST_RESULT_FILE})"
  echo "Test result code is $result"
  exit $result
fi

# --run-tests passed as first argument, run the tests.

# Fail fast during setup.
set -o errexit
set -o pipefail

# Set the required variables if necessary.

if [[ -z ${K8S_USER_OVERRIDE} ]]; then
  export K8S_USER_OVERRIDE=$(gcloud config get-value core/account)
fi

USING_EXISTING_CLUSTER=1
if [[ -z ${K8S_CLUSTER_OVERRIDE} ]]; then
  USING_EXISTING_CLUSTER=0
  export K8S_CLUSTER_OVERRIDE=$(kubectl config current-context)
  acquire_cluster_admin_role ${K8S_USER_OVERRIDE} ${E2E_CLUSTER_NAME} ${E2E_CLUSTER_ZONE}
  # Make sure we're in the default namespace. Currently kubetest switches to
  # test-pods namespace when creating the cluster.
  kubectl config set-context $K8S_CLUSTER_OVERRIDE --namespace=default
fi
readonly USING_EXISTING_CLUSTER

if [[ -z ${DOCKER_REPO_OVERRIDE} ]]; then
  export DOCKER_REPO_OVERRIDE=gcr.io/$(gcloud config get-value project)/ela-e2e-img
fi
export KO_DOCKER_REPO=${DOCKER_REPO_OVERRIDE}

# Build and start Elafros.

echo "- Cluster is ${K8S_CLUSTER_OVERRIDE}"
echo "- User is ${K8S_USER_OVERRIDE}"
echo "- Docker is ${DOCKER_REPO_OVERRIDE}"

header "Building and starting Elafros"
trap teardown EXIT

install_ko

if (( USING_EXISTING_CLUSTER )); then
  echo "Deleting any previous Elafros instance"
  delete_everything
fi
if (( IS_PROW )); then
  gcr_auth
fi

create_everything

# Handle failures ourselves, so we can dump useful info.
set +o errexit
set +o pipefail

wait_until_pods_running ela-system
exit_if_failed

# Run the tests

run_tests conformance pizzaplanet ela-conformance-test
run_tests e2e noodleburg ela-e2e-test

# kubetest teardown might fail and thus incorrectly report failure of the
# script, even if the tests pass.
# We store the real test result to return it later, ignoring any teardown
# failure in kubetest.
# TODO(adrcunha): Get rid of this workaround.
echo -n "0"> ${TEST_RESULT_FILE}
echo "**************************************"
echo "***        ALL TESTS PASSED        ***"
echo "**************************************"
exit 0
