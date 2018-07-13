#!/bin/bash

# Copyright 2018 The Knative Authors
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

# This script runs the end-to-end tests against Knative Serving built from source.
# It is started by prow for each PR.
# For convenience, it can also be executed manually.

# If you already have the *_OVERRIDE environment variables set, call
# this script with the --run-tests arguments and it will start knative in
# the cluster and run the tests.

# Calling this script without arguments will create a new cluster in
# project $PROJECT_ID, start knative in it, run the tests and delete the
# cluster. $DOCKER_REPO_OVERRIDE must point to a valid writable docker repo.

source "$(dirname $(readlink -f ${BASH_SOURCE}))/library.sh"

# Test cluster parameters and location of generated test images
readonly E2E_CLUSTER_NAME=knative-e2e-cluster${BUILD_NUMBER}
readonly E2E_NETWORK_NAME=knative-e2e-net${BUILD_NUMBER}
readonly E2E_CLUSTER_ZONE=us-central1-a
readonly E2E_CLUSTER_NODES=3
readonly E2E_CLUSTER_MACHINE=n1-standard-4
readonly TEST_RESULT_FILE=/tmp/knative-e2e-result
readonly ISTIO_VERSION=0.8.0
readonly ISTIO_DIR=./third_party/istio-${ISTIO_VERSION}/

# This script.
readonly SCRIPT_CANONICAL_PATH="$(readlink -f ${BASH_SOURCE})"

# Helper functions.

function create_istio() {
  kubectl apply -f ${ISTIO_DIR}/istio.yaml
}


function create_monitoring() {
  kubectl apply -R -f config/monitoring/100-common \
    -f config/monitoring/150-elasticsearch-prod \
    -f third_party/config/monitoring/common \
    -f third_party/config/monitoring/elasticsearch \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml
}

function create_everything() {
  create_istio
  kubectl apply -f third_party/config/build/release.yaml
  ko apply -f config/
  create_monitoring
}

function delete_istio() {
  kubectl delete -f ${ISTIO_DIR}/istio.yaml
  kubectl delete clusterrolebinding cluster-admin-binding
}

function delete_monitoring() {
  kubectl delete --ignore-not-found=true -f config/monitoring/100-common \
    -f config/monitoring/150-elasticsearch-prod \
    -f third_party/config/monitoring/common \
    -f third_party/config/monitoring/elasticsearch \
    -f config/monitoring/200-common
}

function delete_everything() {
  delete_monitoring
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

  # Delete Knative Serving images when using prow.
  if (( IS_PROW )); then
    echo "Images in ${DOCKER_REPO_OVERRIDE}:"
    gcloud container images list --repository=${DOCKER_REPO_OVERRIDE}
    delete_gcr_images ${DOCKER_REPO_OVERRIDE}
  else
    # Delete the kubernetes source downloaded by kubetest
    rm -fr kubernetes kubernetes.tar.gz
  fi
}

function abort_if_failed() {
  [[ $? -eq 0 ]] && return 0
  dump_stack_info
  exit 1
}

function dump_stack_info() {
  echo "***************************************"
  echo "***           TEST FAILED           ***"
  echo "***    Start of information dump    ***"
  echo "***************************************"
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
  echo ">>> Knative Serving controller log:"
  kubectl logs $(get_knative_pod controller) -n knative-serving
  echo "***************************************"
  echo "***           TEST FAILED           ***"
  echo "***     End of information dump     ***"
  echo "***************************************"
}

function run_e2e_tests() {
  header "Running tests in $1"
  kubectl create namespace $2
  kubectl label namespace $2 istio-injection=enabled --overwrite
  local options=""
  (( EMIT_METRICS )) && options="-emitmetrics"
  report_go_test -v -tags=e2e -count=1 ./test/$1 -dockerrepo gcr.io/knative-tests/test-images/$3 ${options}

  local result=$?
  [[ ${result} -ne 0 ]] && dump_stack_info
  return ${result}
}

# Script entry point.

cd ${SERVING_ROOT_DIR}

RUN_TESTS=0
EMIT_METRICS=0
for parameter in $@; do
  case $parameter in
    --run-tests)
      RUN_TESTS=1
      shift
      ;;
    --emit-metrics)
      EMIT_METRICS=1
      shift
      ;;
    *)
      echo "error: unknown option ${parameter}"
      echo "usage: $0 [--run-tests][--emit-metrics]"
      exit 1
      ;;
  esac
done
readonly RUN_TESTS
readonly EMIT_METRICS

# Create the test cluster if not running tests.

if (( ! RUN_TESTS )); then
  header "Creating test cluster"
  # Smallest cluster required to run the end-to-end-tests
  CLUSTER_CREATION_ARGS=(
    --gke-create-args="--enable-autoscaling --min-nodes=1 --max-nodes=${E2E_CLUSTER_NODES} --scopes=cloud-platform"
    --gke-shape={\"default\":{\"Nodes\":${E2E_CLUSTER_NODES}\,\"MachineType\":\"${E2E_CLUSTER_MACHINE}\"}}
    --provider=gke
    --deployment=gke
    --cluster="${E2E_CLUSTER_NAME}"
    --gcp-zone="${E2E_CLUSTER_ZONE}"
    --gcp-network="${E2E_NETWORK_NAME}"
    --gke-environment=prod
  )
  if (( ! IS_PROW )); then
    CLUSTER_CREATION_ARGS+=(--gcp-project=${PROJECT_ID:?"PROJECT_ID must be set to the GCP project where the tests are run."})
  fi
  # SSH keys are not used, but kubetest checks for their existence.
  # Touch them so if they don't exist, empty files are create to satisfy the check.
  touch $HOME/.ssh/google_compute_engine.pub
  touch $HOME/.ssh/google_compute_engine
  # Clear user and cluster variables, so they'll be set to the test cluster.
  # DOCKER_REPO_OVERRIDE is not touched because when running locally it must
  # be a writeable docker repo.
  export K8S_USER_OVERRIDE=
  export K8S_CLUSTER_OVERRIDE=
  # Assume test failed (see more details at the end of this script).
  echo -n "1"> ${TEST_RESULT_FILE}
  test_cmd_args="--run-tests"
  (( EMIT_METRICS )) && test_cmd_args+=" --emit-metrics"
  kubetest "${CLUSTER_CREATION_ARGS[@]}" \
    --up \
    --down \
    --extract "gke-${SERVING_GKE_VERSION}" \
    --gcp-node-image ${SERVING_GKE_IMAGE} \
    --test-cmd "${SCRIPT_CANONICAL_PATH}" \
    --test-cmd-args "${test_cmd_args}"
  # Delete target pools and health checks that might have leaked.
  # See https://github.com/knative/serving/issues/959 for details.
  # TODO(adrcunha): Remove once the leak issue is resolved.
  gcp_project=${PROJECT_ID}
  [[ -z ${gcp_project} ]] && gcp_project=$(gcloud config get-value project)
  http_health_checks="$(gcloud compute target-pools list \
    --project=${gcp_project} --format='value(healthChecks)' --filter="instances~-${E2E_CLUSTER_NAME}-" | \
    grep httpHealthChecks | tr '\n' ' ')"
  target_pools="$(gcloud compute target-pools list \
    --project=${gcp_project} --format='value(name)' --filter="instances~-${E2E_CLUSTER_NAME}-" | \
    tr '\n' ' ')"
  region="$(gcloud compute zones list --filter=name=${E2E_CLUSTER_ZONE} --format='value(region)')"
  if [[ -n "${target_pools}" ]]; then
    echo "Found leaked target pools, deleting"
    gcloud compute forwarding-rules delete -q --project=${gcp_project} --region=${region} ${target_pools}
    gcloud compute target-pools delete -q --project=${gcp_project} --region=${region} ${target_pools}
  fi
  if [[ -n "${http_health_checks}" ]]; then
    echo "Found leaked health checks, deleting"
    gcloud compute http-health-checks delete -q --project=${gcp_project} ${http_health_checks}
  fi
  result="$(cat ${TEST_RESULT_FILE})"
  echo "Test result code is $result"
  exit $result
fi

# --run-tests passed, run the tests.

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
  export DOCKER_REPO_OVERRIDE=gcr.io/$(gcloud config get-value project)/knative-e2e-img
fi
export KO_DOCKER_REPO=${DOCKER_REPO_OVERRIDE}

# Build and start Knative Serving.

echo "- Cluster is ${K8S_CLUSTER_OVERRIDE}"
echo "- User is ${K8S_USER_OVERRIDE}"
echo "- Docker is ${DOCKER_REPO_OVERRIDE}"

header "Building and starting Knative Serving"
trap teardown EXIT

if (( USING_EXISTING_CLUSTER )); then
  echo "Deleting any previous Knative Serving instance"
  delete_everything
fi

create_everything

# Handle failures ourselves, so we can dump useful info.
set +o errexit
set +o pipefail

wait_until_pods_running knative-serving || abort_if_failed
wait_until_pods_running istio-system || abort_if_failed
wait_until_service_has_external_ip istio-system knative-ingressgateway
abort_if_failed

# Run the tests

run_e2e_tests conformance pizzaplanet conformance
result=$?
run_e2e_tests e2e noodleburg e2e
[[ $? -ne 0 || ${result} -ne 0 ]] && exit 1

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
