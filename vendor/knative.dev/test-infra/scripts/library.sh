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

# Default GKE version to be used with Knative Serving
readonly SERVING_GKE_VERSION=gke-latest
readonly SERVING_GKE_IMAGE=cos

# Conveniently set GOPATH if unset
if [[ -z "${GOPATH:-}" ]]; then
  export GOPATH="$(go env GOPATH)"
  if [[ -z "${GOPATH}" ]]; then
    echo "WARNING: GOPATH not set and go binary unable to provide it"
  fi
fi

# Useful environment variables
[[ -n "${PROW_JOB_ID:-}" ]] && IS_PROW=1 || IS_PROW=0
readonly IS_PROW
[[ -z "${REPO_ROOT_DIR:-}" ]] && REPO_ROOT_DIR="$(git rev-parse --show-toplevel)"
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
  echo "error: $@"
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
# Parameters: $1 - namespace.
function wait_until_pods_running() {
  echo -n "Waiting until all pods in namespace $1 are up"
  local failed_pod=""
  for i in {1..150}; do  # timeout after 5 minutes
    local pods="$(kubectl get pods --no-headers -n $1 2>/dev/null)"
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
  echo -n "Waiting until all batch jobs in namespace $1 run to completion."
  for i in {1..150}; do  # timeout after 5 minutes
    local jobs=$(kubectl get jobs -n $1 --no-headers \
                 -ocustom-columns='n:{.metadata.name},c:{.spec.completions},s:{.status.succeeded}')
    # All jobs must be complete
    local not_complete=$(echo "${jobs}" | awk '{if ($2!=$3) print $0}' | wc -l)
    if [[ ${not_complete} -eq 0 ]]; then
      echo -e "\nAll jobs are complete:\n${jobs}"
      return 0
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: timeout waiting for jobs to complete\n${jobs}"
  return 1
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

# Sets the given user as cluster admin.
# Parameters: $1 - user
#             $2 - cluster name
#             $3 - cluster region
#             $4 - cluster zone, optional
function acquire_cluster_admin_role() {
  echo "Acquiring cluster-admin role for user '$1'"
  local geoflag="--region=$3"
  [[ -n $4 ]] && geoflag="--zone=$3-$4"
  # Get the password of the admin and use it, as the service account (or the user)
  # might not have the necessary permission.
  local password=$(gcloud --format="value(masterAuth.password)" \
      container clusters describe $2 ${geoflag})
  if [[ -n "${password}" ]]; then
    # Cluster created with basic authentication
    kubectl config set-credentials cluster-admin \
        --username=admin --password=${password}
  else
    local cert=$(mktemp)
    local key=$(mktemp)
    echo "Certificate in ${cert}, key in ${key}"
    gcloud --format="value(masterAuth.clientCertificate)" \
      container clusters describe $2 ${geoflag} | base64 --decode > ${cert}
    gcloud --format="value(masterAuth.clientKey)" \
      container clusters describe $2 ${geoflag} | base64 --decode > ${key}
    kubectl config set-credentials cluster-admin \
      --client-certificate=${cert} --client-key=${key}
  fi
  kubectl config set-context $(kubectl config current-context) \
      --user=cluster-admin
  kubectl create clusterrolebinding cluster-admin-binding \
      --clusterrole=cluster-admin \
      --user=$1
  # Reset back to the default account
  gcloud container clusters get-credentials \
      $2 ${geoflag} --project $(gcloud config get-value project)
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
  local xml="$(mktemp_with_extension ${ARTIFACTS}/junit_XXXXXXXX xml)"
  local failure=""
  if [[ "$3" != "" ]]; then
    # Transform newlines into HTML code.
    # Also escape `<` and `>` as here: https://github.com/golang/go/blob/50bd1c4d4eb4fac8ddeb5f063c099daccfb71b26/src/encoding/json/encode.go#L48, 
    # this is temporary solution for fixing https://github.com/knative/test-infra/issues/1204,
    # which should be obsolete once Test-infra 2.0 is in place
    local msg="$(echo -n "$3" | sed 's/$/\&#xA;/g' | sed 's/</\\u003c/' | sed 's/>/\\u003e/' | sed 's/&/\\u0026/' | tr -d '\n')"
    failure="<failure message=\"Failed\" type=\"\">${msg}</failure>"
  fi
  cat << EOF > "${xml}"
<testsuites>
	<testsuite tests="1" failures="1" time="0.000" name="$1">
		<testcase classname="" name="$2" time="0.0">
			${failure}
		</testcase>
	</testsuite>
</testsuites>
EOF
}

# Runs a go test and generate a junit summary.
# Parameters: $1... - parameters to go test
function report_go_test() {
  # Run tests in verbose mode to capture details.
  # go doesn't like repeating -v, so remove if passed.
  local args=" $@ "
  local go_test="go test -v ${args/ -v / }"
  # Just run regular go tests if not on Prow.
  echo "Running tests with '${go_test}'"
  local report="$(mktemp)"
  capture_output "${report}" ${go_test}
  local failed=$?
  echo "Finished run, return code is ${failed}"
  # Install go-junit-report if necessary.
  run_go_tool github.com/jstemmer/go-junit-report go-junit-report --help > /dev/null 2>&1
  local xml="$(mktemp_with_extension ${ARTIFACTS}/junit_XXXXXXXX xml)"
  cat ${report} \
      | go-junit-report \
      | sed -e "s#\"\(github\.com/knative\|knative\.dev\)/${REPO_NAME}/#\"#g" \
      > ${xml}
  echo "XML report written to ${xml}"
  if [[ -n "$(grep '<testsuites></testsuites>' ${xml})" ]]; then
    # XML report is empty, something's wrong; use the output as failure reason
    create_junit_xml _go_tests "GoTests" "$(cat ${report})"
  fi
  # Capture and report any race condition errors
  local race_errors="$(sed -n '/^WARNING: DATA RACE$/,/^==================$/p' ${report})"
  create_junit_xml _go_tests "DataRaceAnalysis" "${race_errors}"
  if (( ! IS_PROW )); then
    # Keep the suffix, so files are related.
    local logfile=${xml/junit_/go_test_}
    logfile=${logfile/.xml/.log}
    cp ${report} ${logfile}
    echo "Test log written to ${logfile}"
  fi
  return ${failed}
}

# Install Knative Serving in the current cluster.
# Parameters: $1 - Knative Serving manifest.
function start_knative_serving() {
  header "Starting Knative Serving"
  subheader "Installing Knative Serving"
  echo "Installing Serving CRDs from $1"
  kubectl apply --selector knative.dev/crd-install=true -f "$1"
  echo "Installing the rest of serving components from $1"
  kubectl apply -f "$1"
  wait_until_pods_running knative-serving || return 1
}

# Install Knative Monitoring in the current cluster.
# Parameters: $1 - Knative Monitoring manifest.
function start_knative_monitoring() {
  header "Starting Knative Monitoring"
  subheader "Installing Knative Monitoring"
  # namespace istio-system needs to be created first, due to the comment
  # mentioned in
  # https://github.com/knative/serving/blob/4202efc0dc12052edc0630515b101cbf8068a609/config/monitoring/tracing/zipkin/100-zipkin.yaml#L21
  kubectl create namespace istio-system 2>/dev/null
  echo "Installing Monitoring from $1"
  kubectl apply -f "$1" || return 1
  wait_until_pods_running knative-monitoring || return 1
  wait_until_pods_running istio-system || return 1
}

# Install the stable release Knative/serving in the current cluster.
# Parameters: $1 - Knative Serving version number, e.g. 0.6.0.
function start_release_knative_serving() {
  start_knative_serving "https://storage.googleapis.com/knative-releases/serving/previous/v$1/serving.yaml"
}

# Install the latest stable Knative Serving in the current cluster.
function start_latest_knative_serving() {
  start_knative_serving "${KNATIVE_SERVING_RELEASE}"
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

# Run a go tool, installing it first if necessary.
# Parameters: $1 - tool package/dir for go get/install.
#             $2 - tool to run.
#             $3..$n - parameters passed to the tool.
function run_go_tool() {
  local tool=$2
  local action=get
  [[ $1 =~ ^[\./].* ]] && action=install
  # Avoid running `go get` from root dir of the repository, as it can change go.sum and go.mod files.
  # See discussions in https://github.com/golang/go/issues/27643.
  if [[ ${action} == "get" && $(pwd) == "${REPO_ROOT_DIR}" ]]; then
    local temp_dir="$(mktemp -d)"
    local install_failed=0
    # Swallow the output as we are returning the stdout in the end.
    pushd "${temp_dir}" > /dev/null 2>&1
    GOFLAGS="" go ${action} $1 || install_failed=1
    popd > /dev/null 2>&1
    (( install_failed )) && return ${install_failed}
  else
    GOFLAGS="" go ${action} $1
  fi
  shift 2
  ${tool} "$@"
}

# Run go-licenses to update licenses.
# Parameters: $1 - output file, relative to repo root dir.
#             $2 - directory to inspect.
function update_licenses() {
  cd "${REPO_ROOT_DIR}" || return 1
  local dst=$1
  local dir=$2
  shift
  run_go_tool github.com/google/go-licenses go-licenses save "${dir}" --save_path="${dst}" --force || return 1
  # Hack to make sure directories retain write permissions after save. This
  # can happen if the directory being copied is a Go module.
  # See https://github.com/google/go-licenses/issues/11
  chmod -R +w "${dst}"
}

# Run go-licenses to check for forbidden licenses.
function check_licenses() {
  # Check that we don't have any forbidden licenses.
  run_go_tool github.com/google/go-licenses go-licenses check "${REPO_ROOT_DIR}/..." || return 1
}

# Run the given linter on the given files, checking it exists first.
# Parameters: $1 - tool
#             $2 - tool purpose (for error message if tool not installed)
#             $3 - tool parameters (quote if multiple parameters used)
#             $4..$n - files to run linter on
function run_lint_tool() {
  local checker=$1
  local params=$3
  if ! hash ${checker} 2>/dev/null; then
    warning "${checker} not installed, not $2"
    return 127
  fi
  shift 3
  local failed=0
  for file in $@; do
    ${checker} ${params} ${file} || failed=1
  done
  return ${failed}
}

# Check links in the given markdown files.
# Parameters: $1...$n - files to inspect
function check_links_in_markdown() {
  # https://github.com/raviqqe/liche
  local config="${REPO_ROOT_DIR}/test/markdown-link-check-config.rc"
  [[ ! -e ${config} ]] && config="${_TEST_INFRA_SCRIPTS_DIR}/markdown-link-check-config.rc"
  local options="$(grep '^-' ${config} | tr \"\n\" ' ')"
  run_lint_tool liche "checking links in markdown files" "-d ${REPO_ROOT_DIR} ${options}" $@
}

# Check format of the given markdown files.
# Parameters: $1..$n - files to inspect
function lint_markdown() {
  # https://github.com/markdownlint/markdownlint
  local config="${REPO_ROOT_DIR}/test/markdown-lint-config.rc"
  [[ ! -e ${config} ]] && config="${_TEST_INFRA_SCRIPTS_DIR}/markdown-lint-config.rc"
  run_lint_tool mdl "linting markdown files" "-c ${config}" $@
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
    target="$(cd `dirname ${link}` && cd ${target%/*} && echo $PWD/${target##*/})"
    if [[ ${target} != *github.com/knative/* && ${target} != *knative.dev/* ]]; then
      unlink ${link}
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
  echo "$(cd ${path%/*} && echo $PWD/${path##*/})"
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
      gsutil ls gs://knative-releases/${repo_name}/previous/v${major_minor}.*/${yaml_name}.yaml 2> /dev/null \
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

# Initializations that depend on previous functions.
# These MUST come last.

readonly _TEST_INFRA_SCRIPTS_DIR="$(dirname $(get_canonical_path ${BASH_SOURCE[0]}))"
readonly REPO_NAME_FORMATTED="Knative $(capitalize ${REPO_NAME//-/ })"

# Public latest nightly or release yaml files.
readonly KNATIVE_SERVING_RELEASE="$(get_latest_knative_yaml_source "serving" "serving")"
readonly KNATIVE_EVENTING_RELEASE="$(get_latest_knative_yaml_source "eventing" "eventing")"
readonly KNATIVE_MONITORING_RELEASE="$(get_latest_knative_yaml_source "serving" "monitoring")"
