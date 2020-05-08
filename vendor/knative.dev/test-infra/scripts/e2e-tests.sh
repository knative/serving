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

source $(dirname ${BASH_SOURCE})/library.sh

# Build a resource name based on $E2E_BASE_NAME, a suffix and $BUILD_NUMBER.
# Restricts the name length to 40 chars (the limit for resource names in GCP).
# Name will have the form $E2E_BASE_NAME-<PREFIX>$BUILD_NUMBER.
# Parameters: $1 - name suffix
function build_resource_name() {
  local prefix=${E2E_BASE_NAME}-$1
  local suffix=${BUILD_NUMBER}
  # Restrict suffix length to 20 chars
  if [[ -n "${suffix}" ]]; then
    suffix=${suffix:${#suffix}<20?0:-20}
  fi
  local name="${prefix:0:20}${suffix}"
  # Ensure name doesn't end with "-"
  echo "${name%-}"
}

# Test cluster parameters

# Configurable parameters
# export E2E_CLUSTER_REGION and E2E_CLUSTER_ZONE as they're used in the cluster setup subprocess
export E2E_CLUSTER_REGION=${E2E_CLUSTER_REGION:-us-central1}
# By default we use regional clusters.
export E2E_CLUSTER_ZONE=${E2E_CLUSTER_ZONE:-}

# Default backup regions in case of stockouts; by default we don't fall back to a different zone in the same region
readonly E2E_CLUSTER_BACKUP_REGIONS=${E2E_CLUSTER_BACKUP_REGIONS:-us-west1 us-east1}
readonly E2E_CLUSTER_BACKUP_ZONES=${E2E_CLUSTER_BACKUP_ZONES:-}

readonly E2E_CLUSTER_MACHINE=${E2E_CLUSTER_MACHINE:-e2-standard-4}
readonly E2E_GKE_ENVIRONMENT=${E2E_GKE_ENVIRONMENT:-prod}
readonly E2E_GKE_COMMAND_GROUP=${E2E_GKE_COMMAND_GROUP:-beta}

# Each knative repository may have a different cluster size requirement here,
# so we allow calling code to set these parameters.  If they are not set we
# use some sane defaults.
readonly E2E_MIN_CLUSTER_NODES=${E2E_MIN_CLUSTER_NODES:-1}
readonly E2E_MAX_CLUSTER_NODES=${E2E_MAX_CLUSTER_NODES:-3}

readonly E2E_BASE_NAME="k${REPO_NAME}"
readonly E2E_CLUSTER_NAME=$(build_resource_name e2e-cls)
readonly E2E_NETWORK_NAME=$(build_resource_name e2e-net)
readonly TEST_RESULT_FILE=/tmp/${E2E_BASE_NAME}-e2e-result

# Flag whether test is using a boskos GCP project
IS_BOSKOS=0

# Tear down the test resources.
function teardown_test_resources() {
  # On boskos, save time and don't teardown as the cluster will be destroyed anyway.
  (( IS_BOSKOS )) && return
  header "Tearing down test environment"
  function_exists test_teardown && test_teardown
  (( ! SKIP_KNATIVE_SETUP )) && function_exists knative_teardown && knative_teardown
  # Delete the kubernetes source downloaded by kubetest
  rm -fr kubernetes kubernetes.tar.gz
}

# Run the given E2E tests. Assume tests are tagged e2e, unless `-tags=XXX` is passed.
# Parameters: $1..$n - any go test flags, then directories containing the tests to run.
function go_test_e2e() {
  local test_options=""
  local go_options=""
  [[ ! " $@" == *" -tags="* ]] && go_options="-tags=e2e"
  report_go_test -v -race -count=1 ${go_options} $@ ${test_options}
}

# Dump info about the test cluster. If dump_extra_cluster_info() is defined, calls it too.
# This is intended to be called when a test fails to provide debugging information.
function dump_cluster_state() {
  echo "***************************************"
  echo "***         E2E TEST FAILED         ***"
  echo "***    Start of information dump    ***"
  echo "***************************************"

  local output="${ARTIFACTS}/k8s.dump-$(basename ${E2E_SCRIPT}).txt"
  echo ">>> The dump is located at ${output}"

  for crd in $(kubectl api-resources --verbs=list -o name | sort); do
    local count="$(kubectl get $crd --all-namespaces --no-headers 2>/dev/null | wc -l)"
    echo ">>> ${crd} (${count} objects)"
    if [[ "${count}" > "0" ]]; then
      echo ">>> ${crd} (${count} objects)" >> ${output}

      echo ">>> Listing" >> ${output}
      kubectl get ${crd} --all-namespaces >> ${output}

      echo ">>> Details" >> ${output}
      if [[ "${crd}" == "secrets" ]]; then
        echo "Secrets are ignored for security reasons" >> ${output}
      else
        kubectl get ${crd} --all-namespaces -o yaml >> ${output}
      fi
    fi
  done

  if function_exists dump_extra_cluster_state; then
    echo ">>> Extra dump" >> ${output}
    dump_extra_cluster_state >> ${output}
  fi
  echo "***************************************"
  echo "***         E2E TEST FAILED         ***"
  echo "***     End of information dump     ***"
  echo "***************************************"
}

# On a Prow job, save some metadata about the test for Testgrid.
function save_metadata() {
  (( ! IS_PROW )) && return
  local geo_key="Region"
  local geo_value="${E2E_CLUSTER_REGION}"
  if [[ -n "${E2E_CLUSTER_ZONE}" ]]; then
    geo_key="Zone"
    geo_value="${E2E_CLUSTER_REGION}-${E2E_CLUSTER_ZONE}"
  fi
  local cluster_version="$(gcloud container clusters list --project=${E2E_PROJECT_ID} --format='value(currentMasterVersion)')"
  cat << EOF > ${ARTIFACTS}/metadata.json
{
  "E2E:${geo_key}": "${geo_value}",
  "E2E:Machine": "${E2E_CLUSTER_MACHINE}",
  "E2E:Version": "${cluster_version}",
  "E2E:MinNodes": "${E2E_MIN_CLUSTER_NODES}",
  "E2E:MaxNodes": "${E2E_MAX_CLUSTER_NODES}"
}
EOF
}

# Set E2E_CLUSTER_VERSION to a specific GKE version.
# Parameters: $1 - target GKE version (X.Y, X.Y.Z, X.Y.Z-gke.W, default or gke-latest).
#             $2 - region[-zone] where the clusteer will be created.
function resolve_k8s_version() {
  local target_version="$1"
  if [[ "${target_version}" == "default" ]]; then
    local version="$(gcloud container get-server-config \
        --format='value(defaultClusterVersion)' \
        --zone=$2)"
    [[ -z "${version}" ]] && return 1
    E2E_CLUSTER_VERSION="${version}"
    echo "Using default version, ${E2E_CLUSTER_VERSION}"
    return 0
  fi
  # Fetch valid versions
  local versions="$(gcloud container get-server-config \
      --format='value(validMasterVersions)' \
      --zone=$2)"
  [[ -z "${versions}" ]] && return 1
  local gke_versions=($(echo -n "${versions//;/ }"))
  echo "Available GKE versions in $2 are [${versions//;/, }]"
  if [[ "${target_version}" == "gke-latest" ]]; then
    # Get first (latest) version
    E2E_CLUSTER_VERSION="${gke_versions[0]}"
    echo "Using latest version, ${E2E_CLUSTER_VERSION}"
  else
    local latest="$(echo "${gke_versions[@]}" | tr ' ' '\n' | grep -E ^${target_version} | sort -V | tail -1)"
    if [[ -z "${latest}" ]]; then
      echo "ERROR: version ${target_version} is not available"
      return 1
    fi
    E2E_CLUSTER_VERSION="${latest}"
    echo "Using ${E2E_CLUSTER_VERSION} for supplied version ${target_version}"
  fi
  return 0
}

# Create a test cluster with kubetest and call the current script again.
function create_test_cluster() {
  # Fail fast during setup.
  set -o errexit
  set -o pipefail

  if function_exists cluster_setup; then
    cluster_setup || fail_test "cluster setup failed"
  fi

  echo "Cluster will have a minimum of ${E2E_MIN_CLUSTER_NODES} and a maximum of ${E2E_MAX_CLUSTER_NODES} nodes."

  # Smallest cluster required to run the end-to-end-tests
  local CLUSTER_CREATION_ARGS=(
    --gke-create-command="container clusters create --quiet --enable-autoscaling --min-nodes=${E2E_MIN_CLUSTER_NODES} --max-nodes=${E2E_MAX_CLUSTER_NODES} --scopes=cloud-platform --enable-basic-auth --no-issue-client-certificate ${GKE_ADDONS} ${EXTRA_CLUSTER_CREATION_FLAGS[@]}"
    --gke-shape={\"default\":{\"Nodes\":${E2E_MIN_CLUSTER_NODES}\,\"MachineType\":\"${E2E_CLUSTER_MACHINE}\"}}
    --provider=gke
    --deployment=gke
    --cluster="${E2E_CLUSTER_NAME}"
    --gcp-network="${E2E_NETWORK_NAME}"
    --gcp-node-image="${SERVING_GKE_IMAGE}"
    --gke-environment="${E2E_GKE_ENVIRONMENT}"
    --gke-command-group="${E2E_GKE_COMMAND_GROUP}"
    --test=false
    --up
  )
  if (( ! IS_BOSKOS )); then
    CLUSTER_CREATION_ARGS+=(--gcp-project=${GCP_PROJECT})
  fi
  # SSH keys are not used, but kubetest checks for their existence.
  # Touch them so if they don't exist, empty files are create to satisfy the check.
  mkdir -p $HOME/.ssh
  touch $HOME/.ssh/google_compute_engine.pub
  touch $HOME/.ssh/google_compute_engine
  # Assume test failed (see details in set_test_return_code()).
  set_test_return_code 1
  local gcloud_project="${GCP_PROJECT}"
  [[ -z "${gcloud_project}" ]] && gcloud_project="$(gcloud config get-value project)"
  echo "gcloud project is ${gcloud_project}"
  echo "gcloud user is $(gcloud config get-value core/account)"
  (( IS_BOSKOS )) && echo "Using boskos for the test cluster"
  [[ -n "${GCP_PROJECT}" ]] && echo "GCP project for test cluster is ${GCP_PROJECT}"
  echo "Test script is ${E2E_SCRIPT}"
  # Set arguments for this script again
  local test_cmd_args="--run-tests"
  (( SKIP_KNATIVE_SETUP )) && test_cmd_args+=" --skip-knative-setup"
  [[ -n "${GCP_PROJECT}" ]] && test_cmd_args+=" --gcp-project ${GCP_PROJECT}"
  [[ -n "${E2E_SCRIPT_CUSTOM_FLAGS[@]}" ]] && test_cmd_args+=" ${E2E_SCRIPT_CUSTOM_FLAGS[@]}"
  local extra_flags=()
  if (( IS_BOSKOS )); then
    # Add arbitrary duration, wait for Boskos projects acquisition before error out
    extra_flags+=(--boskos-wait-duration=20m)
  elif (( ! SKIP_TEARDOWNS )); then
    # Only let kubetest tear down the cluster if not using Boskos and teardowns are not expected to be skipped,
    # it's done by Janitor if using Boskos
    extra_flags+=(--down)
  fi

  # Set a minimal kubernetes environment that satisfies kubetest
  # TODO(adrcunha): Remove once https://github.com/kubernetes/test-infra/issues/13029 is fixed.
  local kubedir="$(mktemp -d -t kubernetes.XXXXXXXXXX)"
  local test_wrapper="${kubedir}/e2e-test.sh"
  mkdir ${kubedir}/cluster
  ln -s "$(which kubectl)" ${kubedir}/cluster/kubectl.sh
  echo "#!/usr/bin/env bash" > ${test_wrapper}
  echo "cd $(pwd) && set -x" >> ${test_wrapper}
  echo "${E2E_SCRIPT} ${test_cmd_args}" >> ${test_wrapper}
  chmod +x ${test_wrapper}
  cd ${kubedir}

  # Create cluster and run the tests
  create_test_cluster_with_retries "${CLUSTER_CREATION_ARGS[@]}" \
    --test-cmd "${test_wrapper}" \
    ${extra_flags[@]} \
    ${EXTRA_KUBETEST_FLAGS[@]}
  echo "Test subprocess exited with code $?"
  # Ignore any errors below, this is a best-effort cleanup and shouldn't affect the test result.
  set +o errexit
  function_exists cluster_teardown && cluster_teardown
  local result=$(get_test_return_code)
  echo "Artifacts were written to ${ARTIFACTS}"
  echo "Test result code is ${result}"
  exit ${result}
}

# Retry backup regions/zones if cluster creations failed due to stockout.
# Parameters: $1..$n - any kubetest flags other than geo flag.
function create_test_cluster_with_retries() {
  local cluster_creation_log=/tmp/${E2E_BASE_NAME}-cluster_creation-log
  # zone_not_provided is a placeholder for e2e_cluster_zone to make for loop below work
  local zone_not_provided="zone_not_provided"

  local e2e_cluster_regions=(${E2E_CLUSTER_REGION})
  local e2e_cluster_zones=(${E2E_CLUSTER_ZONE})

  if [[ -n "${E2E_CLUSTER_BACKUP_ZONES}" ]]; then
    e2e_cluster_zones+=(${E2E_CLUSTER_BACKUP_ZONES})
  elif [[ -n "${E2E_CLUSTER_BACKUP_REGIONS}" ]]; then
    e2e_cluster_regions+=(${E2E_CLUSTER_BACKUP_REGIONS})
    e2e_cluster_zones=(${zone_not_provided})
  else
    echo "No backup region/zone set, cluster creation will fail in case of stockout"
  fi

  local e2e_cluster_target_version="${E2E_CLUSTER_VERSION}"
  for e2e_cluster_region in "${e2e_cluster_regions[@]}"; do
    for e2e_cluster_zone in "${e2e_cluster_zones[@]}"; do
      E2E_CLUSTER_REGION=${e2e_cluster_region}
      E2E_CLUSTER_ZONE=${e2e_cluster_zone}
      [[ "${E2E_CLUSTER_ZONE}" == "${zone_not_provided}" ]] && E2E_CLUSTER_ZONE=""
      local cluster_creation_zone="${E2E_CLUSTER_REGION}"
      [[ -n "${E2E_CLUSTER_ZONE}" ]] && cluster_creation_zone="${E2E_CLUSTER_REGION}-${E2E_CLUSTER_ZONE}"
      resolve_k8s_version ${e2e_cluster_target_version} ${cluster_creation_zone} || return 1

      header "Creating test cluster ${E2E_CLUSTER_VERSION} in ${cluster_creation_zone}"
      # Don't fail test for kubetest, as it might incorrectly report test failure
      # if teardown fails (for details, see success() below)
      set +o errexit
      export CLUSTER_API_VERSION=${E2E_CLUSTER_VERSION}
      run_go_tool k8s.io/test-infra/kubetest \
        kubetest "$@" --gcp-region=${cluster_creation_zone} 2>&1 | tee ${cluster_creation_log}

      # Exit if test succeeded
      [[ "$(get_test_return_code)" == "0" ]] && return 0
      # Retry if cluster creation failed because of:
      # - stockout (https://github.com/knative/test-infra/issues/592)
      # - latest GKE not available in this region/zone yet (https://github.com/knative/test-infra/issues/694)
      [[ -z "$(grep -Fo 'does not have enough resources available to fulfill' ${cluster_creation_log})" \
          && -z "$(grep -Fo 'ResponseError: code=400, message=No valid versions with the prefix' ${cluster_creation_log})" \
          && -z "$(grep -Po 'ResponseError: code=400, message=Master version "[0-9a-z\-\.]+" is unsupported' ${cluster_creation_log})"  \
          && -z "$(grep -Po 'only \d+ nodes out of \d+ have registered; this is likely due to Nodes failing to start correctly' ${cluster_creation_log})" ]] \
          && return 1
    done
  done
  echo "No more region/zones to try, quitting"
  return 1
}

# Setup the test cluster for running the tests.
function setup_test_cluster() {
  # Fail fast during setup.
  set -o errexit
  set -o pipefail

  header "Test cluster setup"
  kubectl get nodes

  header "Setting up test cluster"

  # Set the actual project the test cluster resides in
  # It will be a project assigned by Boskos if test is running on Prow,
  # otherwise will be ${GCP_PROJECT} set up by user.
  export E2E_PROJECT_ID="$(gcloud config get-value project)"
  readonly E2E_PROJECT_ID

  # Save some metadata about cluster creation for using in prow and testgrid
  save_metadata

  local k8s_user=$(gcloud config get-value core/account)
  local k8s_cluster=$(kubectl config current-context)

  is_protected_cluster ${k8s_cluster} && \
    abort "kubeconfig context set to ${k8s_cluster}, which is forbidden"

  # If cluster admin role isn't set, this is a brand new cluster
  # Setup the admin role and also KO_DOCKER_REPO if it is a GKE cluster
  if [[ -z "$(kubectl get clusterrolebinding cluster-admin-binding 2> /dev/null)" && "${k8s_cluster}" =~ ^gke_.* ]]; then
    acquire_cluster_admin_role ${k8s_user} ${E2E_CLUSTER_NAME} ${E2E_CLUSTER_REGION} ${E2E_CLUSTER_ZONE}
    # Incorporate an element of randomness to ensure that each run properly publishes images.
    export KO_DOCKER_REPO=gcr.io/${E2E_PROJECT_ID}/${E2E_BASE_NAME}-e2e-img/${RANDOM}
  fi

  # Safety checks
  is_protected_gcr ${KO_DOCKER_REPO} && \
    abort "\$KO_DOCKER_REPO set to ${KO_DOCKER_REPO}, which is forbidden"

  # Use default namespace for all subsequent kubectl commands in this context
  kubectl config set-context ${k8s_cluster} --namespace=default

  echo "- gcloud project is ${E2E_PROJECT_ID}"
  echo "- gcloud user is ${k8s_user}"
  echo "- Cluster is ${k8s_cluster}"
  echo "- Docker is ${KO_DOCKER_REPO}"

  export KO_DATA_PATH="${REPO_ROOT_DIR}/.git"

  # Do not run teardowns if we explicitly want to skip them.
  (( ! SKIP_TEARDOWNS )) && trap teardown_test_resources EXIT

  # Handle failures ourselves, so we can dump useful info.
  set +o errexit
  set +o pipefail

  if (( ! SKIP_KNATIVE_SETUP )) && function_exists knative_setup; then
    # Wait for Istio installation to complete, if necessary, before calling knative_setup.
    (( ! SKIP_ISTIO_ADDON )) && (wait_until_batch_job_complete istio-system || return 1)
    knative_setup || fail_test "Knative setup failed"
  fi
  if function_exists test_setup; then
    test_setup || fail_test "test setup failed"
  fi
}

# Gets the exit of the test script.
# For more details, see set_test_return_code().
function get_test_return_code() {
  echo $(cat ${TEST_RESULT_FILE})
}

# Set the return code that the test script will return.
# Parameters: $1 - return code (0-255)
function set_test_return_code() {
  # kubetest teardown might fail and thus incorrectly report failure of the
  # script, even if the tests pass.
  # We store the real test result to return it later, ignoring any teardown
  # failure in kubetest.
  # TODO(adrcunha): Get rid of this workaround.
  echo -n "$1"> ${TEST_RESULT_FILE}
}

# Signal (as return code and in the logs) that all E2E tests passed.
function success() {
  set_test_return_code 0
  echo "**************************************"
  echo "***        E2E TESTS PASSED        ***"
  echo "**************************************"
  exit 0
}

# Exit test, dumping current state info.
# Parameters: $1 - error message (optional).
function fail_test() {
  set_test_return_code 1
  [[ -n $1 ]] && echo "ERROR: $1"
  dump_cluster_state
  exit 1
}

RUN_TESTS=0
SKIP_KNATIVE_SETUP=0
SKIP_ISTIO_ADDON=0
SKIP_TEARDOWNS=0
GCP_PROJECT=""
E2E_SCRIPT=""
E2E_CLUSTER_VERSION=""
GKE_ADDONS=""
EXTRA_CLUSTER_CREATION_FLAGS=()
EXTRA_KUBETEST_FLAGS=()
E2E_SCRIPT_CUSTOM_FLAGS=()

# Parse flags and initialize the test cluster.
function initialize() {
  E2E_SCRIPT="$(get_canonical_path $0)"
  E2E_CLUSTER_VERSION="${SERVING_GKE_VERSION}"

  cd ${REPO_ROOT_DIR}
  while [[ $# -ne 0 ]]; do
    local parameter=$1
    # Try parsing flag as a custom one.
    if function_exists parse_flags; then
      parse_flags $@
      local skip=$?
      if [[ ${skip} -ne 0 ]]; then
        # Skip parsed flag (and possibly argument) and continue
        # Also save it to it's passed through to the test script
        for ((i=1;i<=skip;i++)); do
          E2E_SCRIPT_CUSTOM_FLAGS+=("$1")
          shift
        done
        continue
      fi
    fi
    # Try parsing flag as a standard one.
    case ${parameter} in
      --run-tests) RUN_TESTS=1 ;;
      --skip-knative-setup) SKIP_KNATIVE_SETUP=1 ;;
      --skip-teardowns) SKIP_TEARDOWNS=1 ;;
      --skip-istio-addon) SKIP_ISTIO_ADDON=1 ;;
      *)
        [[ $# -ge 2 ]] || abort "missing parameter after $1"
        shift
        case ${parameter} in
          --gcp-project) GCP_PROJECT=$1 ;;
          --cluster-version) E2E_CLUSTER_VERSION=$1 ;;
          --cluster-creation-flag) EXTRA_CLUSTER_CREATION_FLAGS+=($1) ;;
          --kubetest-flag) EXTRA_KUBETEST_FLAGS+=($1) ;;
          *) abort "unknown option ${parameter}" ;;
        esac
    esac
    shift
  done

  # Use PROJECT_ID if set, unless --gcp-project was used.
  if [[ -n "${PROJECT_ID:-}" && -z "${GCP_PROJECT}" ]]; then
    echo "\$PROJECT_ID is set to '${PROJECT_ID}', using it to run the tests"
    GCP_PROJECT="${PROJECT_ID}"
  fi
  if (( ! IS_PROW )) && (( ! RUN_TESTS )) && [[ -z "${GCP_PROJECT}" ]]; then
    abort "set \$PROJECT_ID or use --gcp-project to select the GCP project where the tests are run"
  fi

  (( IS_PROW )) && [[ -z "${GCP_PROJECT}" ]] && IS_BOSKOS=1

  (( SKIP_ISTIO_ADDON )) || GKE_ADDONS="--addons=Istio"

  readonly RUN_TESTS
  readonly GCP_PROJECT
  readonly IS_BOSKOS
  readonly EXTRA_CLUSTER_CREATION_FLAGS
  readonly EXTRA_KUBETEST_FLAGS
  readonly SKIP_KNATIVE_SETUP
  readonly SKIP_TEARDOWNS
  readonly GKE_ADDONS

  if (( ! RUN_TESTS )); then
    create_test_cluster
  else
    setup_test_cluster
  fi
}
