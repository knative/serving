#!/usr/bin/env bash

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

# This is a helper script for Knative E2E test scripts.
# See README.md for instructions on how to use it.

source $(dirname "${BASH_SOURCE[0]}")/infra-library.sh

readonly TEST_RESULT_FILE=/tmp/${REPO_NAME}-e2e-result

# Flag whether test is using a boskos GCP project
IS_BOSKOS=0

# Tear down the test resources.
function teardown_test_resources() {
  # On boskos, save time and don't teardown as the cluster will be destroyed anyway.
  (( IS_BOSKOS )) && return
  header "Tearing down test environment"
  function_exists test_teardown && test_teardown
  function_exists knative_teardown && knative_teardown
}

# Run the given E2E tests. Assume tests are tagged e2e, unless `-tags=XXX` is passed.
# Parameters: $1..$n - any go test flags, then directories containing the tests to run.
function go_test_e2e() {
  local go_test_args=()
  # Remove empty args as `go test` will consider it as running tests for the current directory, which is not expected.
  for arg in "$@"; do
    [[ -n "$arg" ]] && go_test_args+=("$arg")
  done
  [[ ! " $*" == *" -tags="* ]] && go_test_args+=("-tags=e2e")
  report_go_test -race -count=1 "${go_test_args[@]}"
}

# Setup the test cluster for running the tests.
function setup_test_cluster() {
  # Fail fast during setup.
  set -o errexit
  set -o pipefail

  header "Setting up test cluster"
  kubectl get nodes
  # Set the actual project the test cluster resides in
  # It will be a project assigned by Boskos if test is running on Prow,
  # otherwise will be ${E2E_GCP_PROJECT_ID} set up by user.
  E2E_PROJECT_ID="$(gcloud config get-value project)"
  export E2E_PROJECT_ID
  readonly E2E_PROJECT_ID

  local k8s_cluster
  k8s_cluster=$(kubectl config current-context)

  is_protected_cluster "${k8s_cluster}" && \
    abort "kubeconfig context set to ${k8s_cluster}, which is forbidden"

  # Acquire cluster admin role for the current user.
  acquire_cluster_admin_role "${k8s_cluster}"

  # Setup KO_DOCKER_REPO if it is a GKE cluster. Incorporate an element of randomness to ensure that each run properly publishes images.
  [[ "${k8s_cluster}" =~ ^gke_.* ]] && export KO_DOCKER_REPO=gcr.io/${E2E_PROJECT_ID}/${REPO_NAME}-e2e-img/${RANDOM}

  # Safety checks
  is_protected_gcr "${KO_DOCKER_REPO}" && \
    abort "\$KO_DOCKER_REPO set to ${KO_DOCKER_REPO}, which is forbidden"

  # Use default namespace for all subsequent kubectl commands in this context
  kubectl config set-context "${k8s_cluster}" --namespace=default

  echo "- Cluster is ${k8s_cluster}"
  echo "- Docker is ${KO_DOCKER_REPO}"

  export KO_DATA_PATH="${REPO_ROOT_DIR}/.git"

  # Do not run teardowns if we explicitly want to skip them.
  (( ! SKIP_TEARDOWNS )) && add_trap teardown_test_resources EXIT

  # Handle failures ourselves, so we can dump useful info.
  set +o errexit
  set +o pipefail

  # Wait for Istio installation to complete, if necessary, before calling knative_setup.
  # TODO(chizhg): is it really needed?
  (( ! SKIP_ISTIO_ADDON )) && (wait_until_batch_job_complete istio-system || return 1)
  if function_exists knative_setup; then
    knative_setup || fail_test "Knative setup failed"
  fi
  if function_exists test_setup; then
    test_setup || fail_test "test setup failed"
  fi
}

# Signal (as return code and in the logs) that all E2E tests passed.
function success() {
  echo "**************************************"
  echo "***        E2E TESTS PASSED        ***"
  echo "**************************************"
  dump_metrics
  exit 0
}

# Exit test, dumping current state info.
# Parameters: $1 - error message (optional).
function fail_test() {
  [[ -n $1 ]] && echo "ERROR: $1"
  dump_cluster_state
  dump_metrics
  exit 1
}

SKIP_TEARDOWNS=0
SKIP_ISTIO_ADDON=0
E2E_SCRIPT=""
CLOUD_PROVIDER="gke"

# Parse flags and initialize the test cluster.
function initialize() {
  local run_tests=0
  local custom_flags=()
  E2E_SCRIPT="$(get_canonical_path "$0")"
  local e2e_script_command=( "${E2E_SCRIPT}" "--run-tests" )

  cd "${REPO_ROOT_DIR}"
  while [[ $# -ne 0 ]]; do
    local parameter=$1
    # TODO(chizhg): remove parse_flags logic if no repos are using it.
    # Try parsing flag as a custom one.
    if function_exists parse_flags; then
      parse_flags "$@"
      local skip=$?
      if [[ ${skip} -ne 0 ]]; then
        # Skip parsed flag (and possibly argument) and continue
        # Also save it to it's passed through to the test script
        for ((i=1;i<=skip;i++)); do
          e2e_script_command+=("$1")
          shift
        done
        continue
      fi
    fi
    # Try parsing flag as a standard one.
    case ${parameter} in
      --run-tests) run_tests=1 ;;
      --skip-teardowns) SKIP_TEARDOWNS=1 ;;
      # TODO(chizhg): remove this flag once the addons is defined as an env var.
      --skip-istio-addon) SKIP_ISTIO_ADDON=1 ;;
      *)
        case ${parameter} in
          --cloud-provider) shift; CLOUD_PROVIDER="$1" ;;
          *) custom_flags+=("$parameter") ;;
        esac
    esac
    shift
  done

  (( IS_PROW )) && [[ -z "${GCP_PROJECT_ID:-}" ]] && IS_BOSKOS=1

  if [[ "${CLOUD_PROVIDER}" == "gke" ]]; then
    if (( SKIP_ISTIO_ADDON )); then
      custom_flags+=("--addons=NodeLocalDNS")
    else
      custom_flags+=("--addons=Istio,NodeLocalDNS")
    fi
  fi

  readonly IS_BOSKOS
  readonly SKIP_TEARDOWNS

  if (( ! run_tests )); then
    create_test_cluster "${CLOUD_PROVIDER}" custom_flags e2e_script_command
  else
    setup_test_cluster
  fi
}
