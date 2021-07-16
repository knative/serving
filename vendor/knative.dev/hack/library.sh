#!/usr/bin/env bash

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

# This is a collection of useful bash functions and constants, intended
# to be used in test scripts and the like. It doesn't do anything when
# called from command line.

# GCP project where all tests related resources live
readonly KNATIVE_TESTS_PROJECT=knative-tests

# Conveniently set GOPATH if unset
if [[ ! -v GOPATH ]]; then
  export GOPATH="$(go env GOPATH)"
  if [[ -z "${GOPATH}" ]]; then
    echo "WARNING: GOPATH not set and go binary unable to provide it"
  fi
fi

# Useful environment variables
[[ -v PROW_JOB_ID ]] && IS_PROW=1 || IS_PROW=0
readonly IS_PROW
[[ ! -v REPO_ROOT_DIR ]] && REPO_ROOT_DIR="$(git rev-parse --show-toplevel)"
readonly REPO_ROOT_DIR
readonly REPO_NAME="$(basename ${REPO_ROOT_DIR})"

# Useful flags about the current OS
IS_LINUX=0
IS_OSX=0
IS_WINDOWS=0
case "${OSTYPE}" in
  darwin*) IS_OSX=1 ;;
  linux*) IS_LINUX=1 ;;
  msys*) IS_WINDOWS=1 ;;
  *) echo "** Internal error in library.sh, unknown OS '${OSTYPE}'" ; exit 1 ;;
esac
readonly IS_LINUX
readonly IS_OSX
readonly IS_WINDOWS

# Set ARTIFACTS to an empty temp dir if unset
if [[ -z "${ARTIFACTS:-}" ]]; then
  export ARTIFACTS="$(mktemp -d)"
fi

# On a Prow job, redirect stderr to stdout so it's synchronously added to log
(( IS_PROW )) && exec 2>&1

# Print error message and exit 1
# Parameters: $1..$n - error message to be displayed
function abort() {
  echo "error: $*"
  exit 1
}

# Display a box banner.
# Parameters: $1 - character to use for the box.
#             $2 - banner message.
function make_banner() {
  local msg="$1$1$1$1 $2 $1$1$1$1"
  local border="${msg//[-0-9A-Za-z _.,\/()\']/$1}"
  echo -e "${border}\n${msg}\n${border}"
  # TODO(adrcunha): Remove once logs have timestamps on Prow
  # For details, see https://github.com/kubernetes/test-infra/issues/10100
  echo -e "$1$1$1$1 $(TZ='America/Los_Angeles' date)\n${border}"
}

# Simple header for logging purposes.
function header() {
  local upper="$(echo $1 | tr a-z A-Z)"
  make_banner "=" "${upper}"
}

# Simple subheader for logging purposes.
function subheader() {
  make_banner "-" "$1"
}

# Simple warning banner for logging purposes.
function warning() {
  make_banner "!" "$1"
}

# Checks whether the given function exists.
function function_exists() {
  [[ "$(type -t $1)" == "function" ]]
}

# GitHub Actions aware output grouping.
function group() {
  # End the group is there is already a group.
  if [ -z ${__GROUP_TRACKER+x} ]; then
    export __GROUP_TRACKER="grouping"
    trap end_group EXIT
  else
    end_group
  fi
  # Start a new group.
  start_group "$@"
}

# GitHub Actions aware output grouping.
function start_group() {
  if [[ -n ${GITHUB_WORKFLOW:-} ]]; then
    echo "::group::$@"
    trap end_group EXIT
  else
    echo "--- $@"
  fi
}

# GitHub Actions aware end of output grouping.
function end_group() {
  if [[ -n ${GITHUB_WORKFLOW:-} ]]; then
    echo "::endgroup::"
  fi
}


# Waits until the given object doesn't exist.
# Parameters: $1 - the kind of the object.
#             $2 - object's name.
#             $3 - namespace (optional).
function wait_until_object_does_not_exist() {
  local KUBECTL_ARGS="get $1 $2"
  local DESCRIPTION="$1 $2"

  if [[ -n $3 ]]; then
    KUBECTL_ARGS="get -n $3 $1 $2"
    DESCRIPTION="$1 $3/$2"
  fi
  echo -n "Waiting until ${DESCRIPTION} does not exist"
  for i in {1..150}; do  # timeout after 5 minutes
    if ! kubectl ${KUBECTL_ARGS} > /dev/null 2>&1; then
      echo -e "\n${DESCRIPTION} does not exist"
      return 0
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: timeout waiting for ${DESCRIPTION} not to exist"
  kubectl ${KUBECTL_ARGS}
  return 1
}

# Waits until all pods are running in the given namespace.
# This function handles some edge cases that `kubectl wait` does not support,
# and it provides nice debug info on the state of the pod if it failed,
# that’s why we have this long bash function instead of using `kubectl wait`.
# Parameters: $1 - namespace.
function wait_until_pods_running() {
  echo -n "Waiting until all pods in namespace $1 are up"
  local failed_pod=""
  for i in {1..150}; do  # timeout after 5 minutes
    # List all pods. Ignore Terminating pods as those have either been replaced through
    # a deployment or terminated on purpose (through chaosduck for example).
    local pods="$(kubectl get pods --no-headers -n $1 | grep -v Terminating)"
    # All pods must be running (ignore ImagePull error to allow the pod to retry)
    local not_running_pods=$(echo "${pods}" | grep -v Running | grep -v Completed | grep -v ErrImagePull | grep -v ImagePullBackOff)
    if [[ -n "${pods}" ]] && [[ -z "${not_running_pods}" ]]; then
      # All Pods are running or completed. Verify the containers on each Pod.
      local all_ready=1
      while read pod ; do
        local status=(`echo -n ${pod} | cut -f2 -d' ' | tr '/' ' '`)
        # Set this Pod as the failed_pod. If nothing is wrong with it, then after the checks, set
        # failed_pod to the empty string.
        failed_pod=$(echo -n "${pod}" | cut -f1 -d' ')
        # All containers must be ready
        [[ -z ${status[0]} ]] && all_ready=0 && break
        [[ -z ${status[1]} ]] && all_ready=0 && break
        [[ ${status[0]} -lt 1 ]] && all_ready=0 && break
        [[ ${status[1]} -lt 1 ]] && all_ready=0 && break
        [[ ${status[0]} -ne ${status[1]} ]] && all_ready=0 && break
        # All the tests passed, this is not a failed pod.
        failed_pod=""
      done <<< "$(echo "${pods}" | grep -v Completed)"
      if (( all_ready )); then
        echo -e "\nAll pods are up:\n${pods}"
        return 0
      fi
    elif [[ -n "${not_running_pods}" ]]; then
      # At least one Pod is not running, just save the first one's name as the failed_pod.
      failed_pod="$(echo "${not_running_pods}" | head -n 1 | cut -f1 -d' ')"
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: timeout waiting for pods to come up\n${pods}"
  if [[ -n "${failed_pod}" ]]; then
    echo -e "\n\nFailed Pod (data in YAML format) - ${failed_pod}\n"
    kubectl -n $1 get pods "${failed_pod}" -oyaml
    echo -e "\n\nPod Logs\n"
    kubectl -n $1 logs "${failed_pod}" --all-containers
  fi
  return 1
}

# Waits until all batch jobs complete in the given namespace.
# Parameters: $1 - namespace.
function wait_until_batch_job_complete() {
  echo "Waiting until all batch jobs in namespace $1 run to completion."
  kubectl wait job --for=condition=Complete --all -n "$1" --timeout=5m || return 1
}

# Waits until the given service has an external address (IP/hostname).
# Parameters: $1 - namespace.
#             $2 - service name.
function wait_until_service_has_external_ip() {
  echo -n "Waiting until service $2 in namespace $1 has an external address (IP/hostname)"
  for i in {1..150}; do  # timeout after 15 minutes
    local ip=$(kubectl get svc -n $1 $2 -o jsonpath="{.status.loadBalancer.ingress[0].ip}")
    if [[ -n "${ip}" ]]; then
      echo -e "\nService $2.$1 has IP $ip"
      return 0
    fi
    local hostname=$(kubectl get svc -n $1 $2 -o jsonpath="{.status.loadBalancer.ingress[0].hostname}")
    if [[ -n "${hostname}" ]]; then
      echo -e "\nService $2.$1 has hostname $hostname"
      return 0
    fi
    echo -n "."
    sleep 6
  done
  echo -e "\n\nERROR: timeout waiting for service $2.$1 to have an external address"
  kubectl get pods -n $1
  return 1
}

# Waits until the given service has an external address (IP/hostname) that allow HTTP connections.
# Parameters: $1 - namespace.
#             $2 - service name.
function wait_until_service_has_external_http_address() {
  local ns=$1
  local svc=$2
  local sleep_seconds=6
  local attempts=150

  echo -n "Waiting until service $ns/$svc has an external address (IP/hostname)"
  for attempt in $(seq 1 $attempts); do  # timeout after 15 minutes
    local address=$(kubectl get svc $svc -n $ns -o jsonpath="{.status.loadBalancer.ingress[0].ip}")
    if [[ -n "${address}" ]]; then
      echo -e "Service $ns/$svc has IP $address"
    else
      address=$(kubectl get svc $svc -n $ns -o jsonpath="{.status.loadBalancer.ingress[0].hostname}")
      if [[ -n "${address}" ]]; then
        echo -e "Service $ns/$svc has hostname $address"
      fi
    fi
    if [[ -n "${address}" ]]; then
      local status=$(curl -s -o /dev/null -w "%{http_code}" http://"${address}")
      if [[ $status != "" && $status != "000" ]]; then
        echo -e "$address is ready: prober observed HTTP $status"
        return 0
      else
        echo -e "$address is not ready: prober observed HTTP $status"
      fi
    fi
    echo -n "."
    sleep $sleep_seconds
  done
  echo -e "\n\nERROR: timeout waiting for service $ns/$svc to have an external HTTP address"
  return 1
}

# Waits for the endpoint to be routable.
# Parameters: $1 - External ingress IP address.
#             $2 - cluster hostname.
function wait_until_routable() {
  echo -n "Waiting until cluster $2 at $1 has a routable endpoint"
  for i in {1..150}; do  # timeout after 5 minutes
    local val=$(curl -H "Host: $2" "http://$1" 2>/dev/null)
    if [[ -n "$val" ]]; then
      echo -e "\nEndpoint is now routable"
      return 0
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: Timed out waiting for endpoint to be routable"
  return 1
}

# Returns the name of the first pod of the given app.
# Parameters: $1 - app name.
#             $2 - namespace (optional).
function get_app_pod() {
  local pods=($(get_app_pods $1 $2))
  echo "${pods[0]}"
}

# Returns the name of all pods of the given app.
# Parameters: $1 - app name.
#             $2 - namespace (optional).
function get_app_pods() {
  local namespace=""
  [[ -n $2 ]] && namespace="-n $2"
  kubectl get pods ${namespace} --selector=app=$1 --output=jsonpath="{.items[*].metadata.name}"
}

# Capitalize the first letter of each word.
# Parameters: $1..$n - words to capitalize.
function capitalize() {
  local capitalized=()
  for word in $@; do
    local initial="$(echo ${word:0:1}| tr 'a-z' 'A-Z')"
    capitalized+=("${initial}${word:1}")
  done
  echo "${capitalized[@]}"
}

# Dumps pod logs for the given app.
# Parameters: $1 - app name.
#             $2 - namespace.
function dump_app_logs() {
  echo ">>> ${REPO_NAME_FORMATTED} $1 logs:"
  for pod in $(get_app_pods "$1" "$2")
  do
    echo ">>> Pod: $pod"
    kubectl -n "$2" logs "$pod" --all-containers
  done
}

# Run a command through tee and capture its output.
# Parameters: $1 - file where the output will be stored.
#             $2... - command to run.
function capture_output() {
  local report="$1"
  shift
  "$@" 2>&1 | tee "${report}"
  local failed=( ${PIPESTATUS[@]} )
  [[ ${failed[0]} -eq 0 ]] && failed=${failed[1]} || failed=${failed[0]}
  return ${failed}
}

# Print failed step, which could be highlighted by spyglass.
# Parameters: $1...n - description of step that failed
function step_failed() {
  local spyglass_token="Step failed:"
  echo "${spyglass_token} $@"
}

# Create a temporary file with the given extension in a way that works on both Linux and macOS.
# Parameters: $1 - file name without extension (e.g. 'myfile_XXXX')
#             $2 - file extension (e.g. 'xml')
function mktemp_with_extension() {
  local nameprefix
  local fullname

  nameprefix="$(mktemp $1)"
  fullname="${nameprefix}.$2"
  mv ${nameprefix} ${fullname}

  echo ${fullname}
}

# Create a JUnit XML for a test.
# Parameters: $1 - check class name as an identifier (e.g. BuildTests)
#             $2 - check name as an identifier (e.g., GoBuild)
#             $3 - failure message (can contain newlines), optional (means success)
function create_junit_xml() {
  local xml
  xml="$(mktemp_with_extension "${ARTIFACTS}"/junit_XXXXXXXX xml)"
  echo "JUnit file ${xml} is created for reporting the test result"
  run_kntest junit --suite="$1" --name="$2" --err-msg="$3" --dest="${xml}" || return 1
}

# Runs a go test and generate a junit summary.
# Parameters: $1... - parameters to go test
function report_go_test() {
  local go_test_args=( "$@" )
  # Install gotestsum if necessary.
  run_go_tool gotest.tools/gotestsum gotestsum --help > /dev/null 2>&1
  # Capture the test output to the report file.
  local report
  report="$(mktemp)"
  local xml
  xml="$(mktemp_with_extension "${ARTIFACTS}"/junit_XXXXXXXX xml)"
  echo "Running go test with args: ${go_test_args[*]}"
  capture_output "${report}" gotestsum --format "${GO_TEST_VERBOSITY:-testname}" \
    --junitfile "${xml}" --junitfile-testsuite-name relative --junitfile-testcase-classname relative \
    -- "${go_test_args[@]}"
  local failed=$?
  echo "Finished run, return code is ${failed}"

  echo "XML report written to ${xml}"
  if [[ -n "$(grep '<testsuites></testsuites>' "${xml}")" ]]; then
    # XML report is empty, something's wrong; use the output as failure reason
    create_junit_xml _go_tests "GoTests" "$(cat "${report}")"
  fi
  # Capture and report any race condition errors
  local race_errors
  race_errors="$(sed -n '/^WARNING: DATA RACE$/,/^==================$/p' "${report}")"
  create_junit_xml _go_tests "DataRaceAnalysis" "${race_errors}"
  if (( ! IS_PROW )); then
    # Keep the suffix, so files are related.
    local logfile=${xml/junit_/go_test_}
    logfile=${logfile/.xml/.log}
    cp "${report}" "${logfile}"
    echo "Test log written to ${logfile}"
  fi
  return ${failed}
}

# Install Knative Serving in the current cluster.
# Parameters: $1 - Knative Serving crds manifest.
#             $2 - Knative Serving core manifest.
#             $3 - Knative net-istio manifest.
function start_knative_serving() {
  header "Starting Knative Serving"
  subheader "Installing Knative Serving"
  echo "Installing Serving CRDs from $1"
  kubectl apply -f "$1"
  echo "Installing Serving core components from $2"
  kubectl apply -f "$2"
  echo "Installing net-istio components from $3"
  kubectl apply -f "$3"
  wait_until_pods_running knative-serving || return 1
}

# Install the stable release Knative/serving in the current cluster.
# Parameters: $1 - Knative Serving version number, e.g. 0.6.0.
function start_release_knative_serving() {
  start_knative_serving "https://storage.googleapis.com/knative-releases/serving/previous/v$1/serving-crds.yaml" \
    "https://storage.googleapis.com/knative-releases/serving/previous/v$1/serving-core.yaml" \
    "https://storage.googleapis.com/knative-releases/net-istio/previous/v$1/net-istio.yaml"
}

# Install the latest stable Knative Serving in the current cluster.
function start_latest_knative_serving() {
  start_knative_serving "${KNATIVE_SERVING_RELEASE_CRDS}" "${KNATIVE_SERVING_RELEASE_CORE}" "${KNATIVE_NET_ISTIO_RELEASE}"
}

# Install Knative Eventing in the current cluster.
# Parameters: $1 - Knative Eventing manifest.
function start_knative_eventing() {
  header "Starting Knative Eventing"
  subheader "Installing Knative Eventing"
  echo "Installing Eventing CRDs from $1"
  kubectl apply --selector knative.dev/crd-install=true -f "$1"
  echo "Installing the rest of eventing components from $1"
  kubectl apply -f "$1"
  wait_until_pods_running knative-eventing || return 1
}

# Install the stable release Knative/eventing in the current cluster.
# Parameters: $1 - Knative Eventing version number, e.g. 0.6.0.
function start_release_knative_eventing() {
  start_knative_eventing "https://storage.googleapis.com/knative-releases/eventing/previous/v$1/eventing.yaml"
}

# Install the latest stable Knative Eventing in the current cluster.
function start_latest_knative_eventing() {
  start_knative_eventing "${KNATIVE_EVENTING_RELEASE}"
}

# Install Knative Eventing extension in the current cluster.
# Parameters: $1 - Knative Eventing extension manifest.
#             $2 - Namespace to look for ready pods into
function start_knative_eventing_extension() {
  header "Starting Knative Eventing Extension"
  echo "Installing Extension CRDs from $1"
  kubectl apply -f "$1"
  wait_until_pods_running "$2" || return 1
}

# Install the stable release of eventing extension sugar controller in the current cluster.
# Parameters: $1 - Knative Eventing release version, e.g. 0.16.0
function start_release_eventing_sugar_controller() {
  start_knative_eventing_extension "https://storage.googleapis.com/knative-releases/eventing/previous/v$1/eventing-sugar-controller.yaml" "knative-eventing"
}

# Install the sugar cotroller eventing extension
function start_latest_eventing_sugar_controller() {
  start_knative_eventing_extension "${KNATIVE_EVENTING_SUGAR_CONTROLLER_RELEASE}" "knative-eventing"
}

# Run a go tool, installing it first if necessary.
# Parameters: $1 - tool package/dir for go get/install.
#             $2 - tool to run.
#             $3..$n - parameters passed to the tool.
function run_go_tool() {
  local tool=$2
  local install_failed=0
  if [[ -z "$(which ${tool})" ]]; then
    local action=get
    [[ $1 =~ ^[\./].* ]] && action=install
    # Avoid running `go get` from root dir of the repository, as it can change go.sum and go.mod files.
    # See discussions in https://github.com/golang/go/issues/27643.
    if [[ ${action} == "get" && $(pwd) == "${REPO_ROOT_DIR}" ]]; then
      local temp_dir="$(mktemp -d)"
      # Swallow the output as we are returning the stdout in the end.
      pushd "${temp_dir}" > /dev/null 2>&1
      GOFLAGS="" go ${action} "$1" || install_failed=1
      popd > /dev/null 2>&1
    else
      GOFLAGS="" go ${action} "$1" || install_failed=1
    fi
  fi
  (( install_failed )) && return ${install_failed}
  shift 2
  ${tool} "$@"
}

# Add function call to trap
# Parameters: $1 - Function to call
#             $2...$n - Signals for trap
function add_trap {
  local cmd=$1
  shift
  for trap_signal in "$@"; do
    local current_trap
    current_trap="$(trap -p "$trap_signal" | cut -d\' -f2)"
    local new_cmd="($cmd)"
    [[ -n "${current_trap}" ]] && new_cmd="${current_trap};${new_cmd}"
    trap -- "${new_cmd}" "$trap_signal"
  done
}

# Update go deps.
# Parameters (parsed as flags):
#   "--upgrade", bool, do upgrade.
#   "--release <version>" used with upgrade. The release version to upgrade
#                         Knative components. ex: --release v0.18. Defaults to
#                         "master".
# Additional dependencies can be included in the upgrade by providing them in a
# global env var: FLOATING_DEPS
# --upgrade will set GOPROXY to direct unless it is already set.
function go_update_deps() {
  cd "${REPO_ROOT_DIR}" || return 1

  export GO111MODULE=on
  export GOFLAGS=""
  export GONOSUMDB="${GONOSUMDB:-},knative.dev/*"
  export GONOPROXY="${GONOPROXY:-},knative.dev/*"

  echo "=== Update Deps for Golang"

  local UPGRADE=0
  local VERSION="v9000.1" # release v9000 is so far in the future, it will always pick the default branch.
  local DOMAIN="knative.dev"
  while [[ $# -ne 0 ]]; do
    parameter=$1
    case ${parameter} in
      --upgrade) UPGRADE=1 ;;
      --release) shift; VERSION="$1" ;;
      --domain) shift; DOMAIN="$1" ;;
      *) abort "unknown option ${parameter}" ;;
    esac
    shift
  done

  if [[ $UPGRADE == 1 ]]; then
    group "Upgrading to ${VERSION}"
    FLOATING_DEPS+=( $(run_go_tool knative.dev/test-infra/buoy buoy float ${REPO_ROOT_DIR}/go.mod --release ${VERSION} --domain ${DOMAIN}) )
    if [[ ${#FLOATING_DEPS[@]} > 0 ]]; then
      echo "Floating deps to ${FLOATING_DEPS[@]}"
      go get -d ${FLOATING_DEPS[@]}
    else
      echo "Nothing to upgrade."
    fi
  fi

  group "Go mod tidy and vendor"

  # Prune modules.
  local orig_pipefail_opt=$(shopt -p -o pipefail)
  set -o pipefail
  go mod tidy 2>&1 | grep -v "ignoring symlink" || true
  go mod vendor 2>&1 |  grep -v "ignoring symlink" || true
  eval "$orig_pipefail_opt"

  group "Removing unwanted vendor files"

  # Remove unwanted vendor files
  find vendor/ \( -name "OWNERS" \
    -o -name "OWNERS_ALIASES" \
    -o -name "BUILD" \
    -o -name "BUILD.bazel" \
    -o -name "*_test.go" \) -exec rm -f {} +

  export GOFLAGS=-mod=vendor

  group "Updating licenses"
  update_licenses third_party/VENDOR-LICENSE "./..."

  group "Removing broken symlinks"
  remove_broken_symlinks ./vendor
}

# Return the go module name of the current module.
# Intended to be used like:
#   export MODULE_NAME=$(go_mod_module_name)
function go_mod_module_name() {
  go mod graph | cut -d' ' -f 1 | grep -v '@' | head -1
}

# Return a GOPATH to a temp directory. Works around the out-of-GOPATH issues
# for k8s client gen mixed with go mod.
# Intended to be used like:
#   export GOPATH=$(go_mod_gopath_hack)
function go_mod_gopath_hack() {
    # Skip this if the directory is already checked out onto the GOPATH.
  if [[ "${REPO_ROOT_DIR##$(go env GOPATH)}" != "$REPO_ROOT_DIR" ]]; then
    go env GOPATH
    return
  fi

  local TMP_DIR="$(mktemp -d)"
  local TMP_REPO_PATH="${TMP_DIR}/src/$(go_mod_module_name)"
  mkdir -p "$(dirname "${TMP_REPO_PATH}")" && ln -s "${REPO_ROOT_DIR}" "${TMP_REPO_PATH}"

  echo "${TMP_DIR}"
}

# Run kntest tool, error out and ask users to install it if it's not currently installed.
# Parameters: $1..$n - parameters passed to the tool.
function run_kntest() {
  if [[ ! -x "$(command -v kntest)" ]]; then
    echo "--- FAIL: kntest not installed, please clone knative test-infra repo and run \`go install ./kntest/cmd/kntest\` to install it"; return 1;
  fi
  kntest "$@"
}

# Run go-licenses to update licenses.
# Parameters: $1 - output file, relative to repo root dir.
#             $2 - directory to inspect.
function update_licenses() {
  cd "${REPO_ROOT_DIR}" || return 1
  local dst=$1
  local dir=$2
  shift
  run_go_tool github.com/google/go-licenses go-licenses save "${dir}" --save_path="${dst}" --force || \
    { echo "--- FAIL: go-licenses failed to update licenses"; return 1; }
}

# Run go-licenses to check for forbidden licenses.
function check_licenses() {
  # Check that we don't have any forbidden licenses.
  run_go_tool github.com/google/go-licenses go-licenses check "${REPO_ROOT_DIR}/..." || \
    { echo "--- FAIL: go-licenses failed the license check"; return 1; }
}

# Return whether the given parameter is an integer.
# Parameters: $1 - integer to check
function is_int() {
  [[ -n $1 && $1 =~ ^[0-9]+$ ]]
}

# Return whether the given parameter is the knative release/nightly GCF.
# Parameters: $1 - full GCR name, e.g. gcr.io/knative-foo-bar
function is_protected_gcr() {
  [[ -n $1 && $1 =~ ^gcr.io/knative-(releases|nightly)/?$ ]]
}

# Return whether the given parameter is any cluster under ${KNATIVE_TESTS_PROJECT}.
# Parameters: $1 - Kubernetes cluster context (output of kubectl config current-context)
function is_protected_cluster() {
  # Example: gke_knative-tests_us-central1-f_prow
  [[ -n $1 && $1 =~ ^gke_${KNATIVE_TESTS_PROJECT}_us\-[a-zA-Z0-9]+\-[a-z]+_[a-z0-9\-]+$ ]]
}

# Return whether the given parameter is ${KNATIVE_TESTS_PROJECT}.
# Parameters: $1 - project name
function is_protected_project() {
  [[ -n $1 && "$1" == "${KNATIVE_TESTS_PROJECT}" ]]
}

# Remove symlinks in a path that are broken or lead outside the repo.
# Parameters: $1 - path name, e.g. vendor
function remove_broken_symlinks() {
  for link in $(find $1 -type l); do
    # Remove broken symlinks
    if [[ ! -e ${link} ]]; then
      unlink ${link}
      continue
    fi
    # Get canonical path to target, remove if outside the repo
    local target="$(ls -l ${link})"
    target="${target##* -> }"
    [[ ${target} == /* ]] || target="./${target}"
    target="$(cd `dirname "${link}"` && cd "${target%/*}" && echo "$PWD"/"${target##*/}")"
    if [[ ${target} != *github.com/knative/* && ${target} != *knative.dev/* ]]; then
      unlink "${link}"
      continue
    fi
  done
}

# Returns the canonical path of a filesystem object.
# Parameters: $1 - path to return in canonical form
#             $2 - base dir for relative links; optional, defaults to current
function get_canonical_path() {
  # We don't use readlink because it's not available on every platform.
  local path=$1
  local pwd=${2:-.}
  [[ ${path} == /* ]] || path="${pwd}/${path}"
  echo "$(cd "${path%/*}" && echo "$PWD"/"${path##*/}")"
}

# List changed files in the current PR.
# This is implemented as a function so it can be mocked in unit tests.
# It will fail if a file name ever contained a newline character (which is bad practice anyway)
function list_changed_files() {
  if [[ -v PULL_BASE_SHA ]] && [[ -v PULL_PULL_SHA ]]; then
    # Avoid warning when there are more than 1085 files renamed:
    # https://stackoverflow.com/questions/7830728/warning-on-diff-renamelimit-variable-when-doing-git-push
    git config diff.renames 0
    git --no-pager diff --name-only "${PULL_BASE_SHA}".."${PULL_PULL_SHA}"
  else
    # Do our best if not running in Prow
    git diff --name-only HEAD^
  fi
}

# Returns the current branch.
function current_branch() {
  local branch_name=""
  # Get the branch name from Prow's env var, see https://github.com/kubernetes/test-infra/blob/master/prow/jobs.md.
  # Otherwise, try getting the current branch from git.
  (( IS_PROW )) && branch_name="${PULL_BASE_REF:-}"
  [[ -z "${branch_name}" ]] && branch_name="$(git rev-parse --abbrev-ref HEAD)"
  echo "${branch_name}"
}

# Returns whether the current branch is a release branch.
function is_release_branch() {
  [[ $(current_branch) =~ ^release-[0-9\.]+$ ]]
}

# Returns the URL to the latest manifest for the given Knative project.
# Parameters: $1 - repository name of the given project
#             $2 - name of the yaml file, without extension
function get_latest_knative_yaml_source() {
  local repo_name="$1"
  local yaml_name="$2"
  # If it's a release branch, the yaml source URL should point to a specific version.
  if is_release_branch; then
    # Extract the release major&minor version from the branch name.
    local branch_name="$(current_branch)"
    local major_minor="${branch_name##release-}"
    # Find the latest release manifest with the same major&minor version.
    local yaml_source_path="$(
      gsutil ls "gs://knative-releases/${repo_name}/previous/v${major_minor}.*/${yaml_name}.yaml" 2> /dev/null \
      | sort \
      | tail -n 1 \
      | cut -b6-)"
    # The version does exist, return it.
    if [[ -n "${yaml_source_path}" ]]; then
      echo "https://storage.googleapis.com/${yaml_source_path}"
      return
    fi
    # Otherwise, fall back to nightly.
  fi
  echo "https://storage.googleapis.com/knative-nightly/${repo_name}/latest/${yaml_name}.yaml"
}

function shellcheck_new_files() {
  declare -a array_of_files
  local failed=0

  if [ -z "$SHELLCHECK_IGNORE_FILES" ]; then
    SHELLCHECK_IGNORE_FILES="^vendor/"
  fi

  readarray -t array_of_files < <(list_changed_files)
  for filename in "${array_of_files[@]}"; do
    if echo "${filename}" | grep -q "$SHELLCHECK_IGNORE_FILES"; then
      continue
    fi
    if file "${filename}" | grep -q "shell script"; then
      # SC1090 is "Can't follow non-constant source"; we will scan files individually
      if shellcheck -e SC1090 "${filename}"; then
        echo "--- PASS: shellcheck on ${filename}"
      else
        echo "--- FAIL: shellcheck on ${filename}"
        failed=1
      fi
    fi
  done
  if [[ ${failed} -eq 1 ]]; then
    fail_script "shellcheck failures"
  fi
}

function latest_version() {
  # This function works "best effort" and works on Prow but not necessarily locally.
  # The problem is finding the latest release. If a release occurs on the same commit which
  # was branched from main, then the tag will be an ancestor to any commit derived from main.
  # That was the original logic. Additionally in a release branch, the tag is always an ancestor.
  # However, if the release commit ends up not the first commit from main, then the tag is not
  # an ancestor of main, so we can't use `git describe` to find the most recent versioned tag. So
  # we just sort all the tags and find the newest versioned one.
  # But when running locally, we cannot(?) know if the current branch is a fork of main or a fork
  # of a release branch. That's where this function will malfunction when the last release did not
  # occur on the first commit -- it will try to run the upgrade tests from an older version instead
  # of the most recent release.
  # Workarounds include:
  # Tag the first commit of the release branch. Say release-0.75 released v0.75.0 from the second commit
  # Then tag the first commit in common between main and release-0.75 with `v0.75`.
  # Always name your local fork master or main.
  if [ $(current_branch) = "master" ] || [ $(current_branch) = "main" ]; then
    # For main branch, simply use git tag without major version, this will work even
    # if the release tag is not in the main
    git tag -l "v[0-9]*" | sort -r --version-sort | head -n1
  else
    local semver=$(git describe --match "v[0-9]*" --abbrev=0)
    local major_minor=$(echo "$semver" | cut -d. -f1-2)

    # Get the latest patch release for the major minor
    git tag -l "${major_minor}*" | sort -r --version-sort | head -n1
  fi
}

# Initializations that depend on previous functions.
# These MUST come last.

readonly _TEST_INFRA_SCRIPTS_DIR="$(dirname $(get_canonical_path "${BASH_SOURCE[0]}"))"
readonly REPO_NAME_FORMATTED="Knative $(capitalize "${REPO_NAME//-/ }")"

# Public latest nightly or release yaml files.
readonly KNATIVE_SERVING_RELEASE_CRDS="$(get_latest_knative_yaml_source "serving" "serving-crds")"
readonly KNATIVE_SERVING_RELEASE_CORE="$(get_latest_knative_yaml_source "serving" "serving-core")"
readonly KNATIVE_NET_ISTIO_RELEASE="$(get_latest_knative_yaml_source "net-istio" "net-istio")"
readonly KNATIVE_EVENTING_RELEASE="$(get_latest_knative_yaml_source "eventing" "eventing")"
readonly KNATIVE_EVENTING_SUGAR_CONTROLLER_RELEASE="$(get_latest_knative_yaml_source "eventing" "eventing-sugar-controller")"
