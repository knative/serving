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

# This is a collection of useful bash functions and constants, intended
# to be used in test scripts and the like. It doesn't do anything when
# called from command line.

# Default GKE version to be used with Knative Serving
readonly SERVING_GKE_VERSION=latest
readonly SERVING_GKE_IMAGE=cos

# Public latest stable nightly images and yaml files.
readonly KNATIVE_BASE_YAML_SOURCE=https://storage.googleapis.com/knative-nightly/@/latest
readonly KNATIVE_ISTIO_CRD_YAML=${KNATIVE_BASE_YAML_SOURCE/@/serving}/istio-crds.yaml
readonly KNATIVE_ISTIO_YAML=${KNATIVE_BASE_YAML_SOURCE/@/serving}/istio.yaml
readonly KNATIVE_SERVING_RELEASE=${KNATIVE_BASE_YAML_SOURCE/@/serving}/release.yaml
readonly KNATIVE_BUILD_RELEASE=${KNATIVE_BASE_YAML_SOURCE/@/build}/release.yaml
readonly KNATIVE_EVENTING_RELEASE=${KNATIVE_BASE_YAML_SOURCE/@/eventing}/release.yaml

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
readonly REPO_ROOT_DIR="$(git rev-parse --show-toplevel)"
readonly REPO_NAME="$(basename ${REPO_ROOT_DIR})"

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
    local border="${msg//[-0-9A-Za-z _.,\/()]/$1}"
    echo -e "${border}\n${msg}\n${border}"
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
  for i in {1..150}; do  # timeout after 5 minutes
    local pods="$(kubectl get pods --no-headers -n $1 2>/dev/null)"
    # All pods must be running
    local not_running=$(echo "${pods}" | grep -v Running | grep -v Completed | wc -l)
    if [[ -n "${pods}" && ${not_running} -eq 0 ]]; then
      local all_ready=1
      while read pod ; do
        local status=(`echo -n ${pod} | cut -f2 -d' ' | tr '/' ' '`)
        # All containers must be ready
        [[ -z ${status[0]} ]] && all_ready=0 && break
        [[ -z ${status[1]} ]] && all_ready=0 && break
        [[ ${status[0]} -lt 1 ]] && all_ready=0 && break
        [[ ${status[1]} -lt 1 ]] && all_ready=0 && break
        [[ ${status[0]} -ne ${status[1]} ]] && all_ready=0 && break
      done <<< $(echo "${pods}" | grep -v Completed)
      if (( all_ready )); then
        echo -e "\nAll pods are up:\n${pods}"
        return 0
      fi
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: timeout waiting for pods to come up\n${pods}"
  kubectl get pods -n $1
  return 1
}

# Waits until the given service has an external IP address.
# Parameters: $1 - namespace.
#             $2 - service name.
function wait_until_service_has_external_ip() {
  echo -n "Waiting until service $2 in namespace $1 has an external IP"
  for i in {1..150}; do  # timeout after 15 minutes
    local ip=$(kubectl get svc -n $1 $2 -o jsonpath="{.status.loadBalancer.ingress[0].ip}")
    if [[ -n "${ip}" ]]; then
      echo -e "\nService $2.$1 has IP $ip"
      return 0
    fi
    echo -n "."
    sleep 6
  done
  echo -e "\n\nERROR: timeout waiting for service $svc.$ns to have an external IP"
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
      echo "\nEndpoint is now routable"
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

# Sets the given user as cluster admin.
# Parameters: $1 - user
#             $2 - cluster name
#             $3 - cluster region
function acquire_cluster_admin_role() {
  # Get the password of the admin and use it, as the service account (or the user)
  # might not have the necessary permission.
  local password=$(gcloud --format="value(masterAuth.password)" \
      container clusters describe $2 --region=$3)
  if [[ -n "${password}" ]]; then
    # Cluster created with basic authentication
    kubectl config set-credentials cluster-admin \
        --username=admin --password=${password}
  else
    local cert=$(mktemp)
    local key=$(mktemp)
    echo "Certificate in ${cert}, key in ${key}"
    gcloud --format="value(masterAuth.clientCertificate)" \
      container clusters describe $2 --region=$3 | base64 -d > ${cert}
    gcloud --format="value(masterAuth.clientKey)" \
      container clusters describe $2 --region=$3 | base64 -d > ${key}
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
      $2 --region=$3 --project $(gcloud config get-value project)
}

# Runs a go test and generate a junit summary through bazel.
# Parameters: $1... - parameters to go test
function report_go_test() {
  # Run tests in verbose mode to capture details.
  # go doesn't like repeating -v, so remove if passed.
  local args=" $@ "
  local go_test="go test -race -v ${args/ -v / }"
  # Just run regular go tests if not on Prow.
  if (( ! IS_PROW )); then
    ${go_test}
    return
  fi
  echo "Running tests with '${go_test}'"
  local report=$(mktemp)
  local failed=0
  local test_count=0
  local tests_failed=0
  ${go_test} > ${report} || failed=$?
  echo "Finished run, return code is ${failed}"
  # Tests didn't run.
  [[ ! -s ${report} ]] && return 1
  # Create WORKSPACE file, required to use bazel, if necessary.
  touch WORKSPACE
  local targets=""
  local last_run=""
  local test_files=""
  local summary=$(mktemp)
  # Parse the report and generate fake tests for each passing/failing test.
  while read line ; do
    local fields=(`echo -n ${line}`)
    local field0="${fields[0]}"
    local field1="${fields[1]}"
    local name="${fields[2]}"
    # Deal with a SIGQUIT or panic log entry (usually a test timeout).
    # This is a fallback in case there's no kill signal log entry.
    # SIGQUIT: quit
    # panic: test timed out after 5m0s
    if [[ "${field0}" == "SIGQUIT:" || "${field0}" == "panic:" ]]; then
      name="${last_run}"
      field1="FAIL:"
      error="${fields[@]}"
    fi
    # Ignore subtests (those containing slashes)
    if [[ -n "${name##*/*}" ]]; then
      local error=""
      # Deal with a kill signal log entry (usually a test timeout).
      # *** Test killed with quit: ran too long (10m0s).
      if [[ "${field0}" == "***" ]]; then
        name="${last_run}"
        field1="FAIL:"
        error="${fields[@]:1}"
      fi
      # Deal with a fatal log entry, which has a different format:
      # fatal   TestFoo   foo_test.go:275 Expected "foo" but got "bar"
      if [[ "${field0}" == "fatal" ]]; then
        name="${field1}"
        field1="FAIL:"
        error="${fields[@]:3}"
      fi
      # Keep track of the test currently running.
      if [[ "${field1}" == "RUN" ]]; then
        last_run="${name}"
      fi
      # Handle regular go test pass/fail entry for a test.
      if [[ "${field1}" == "PASS:" || "${field1}" == "FAIL:" ]]; then
        local status="> pass"
        [[ "${field1}" == "FAIL:" ]] && status="X FAIL"
        echo "  ${status} ${name}" >> ${summary}
        test_count=$(( test_count + 1 ))
        local src="${name}.sh"
        echo "exit 0" > ${src}
        if [[ "${field1}" == "FAIL:" ]]; then
          tests_failed=$(( tests_failed + 1 ))
          [[ -z "${error}" ]] && read error
          echo "cat <<ERROR-EOF" > ${src}
          echo "${error}" >> ${src}
          echo "ERROR-EOF" >> ${src}
          echo "exit 1" >> ${src}
        fi
        chmod +x ${src}
        test_files="${test_files} ${src}"
        # Populate BUILD.bazel
        echo "sh_test(name=\"${name}\", srcs=[\"${src}\"])" >> BUILD.bazel
      elif [[ "${field0}" == "FAIL" || "${field0}" == "ok" ]] && [[ -n "${field1}" ]]; then
        local status="> pass"
        [[ "${field0}" == "FAIL" ]] && status="X FAIL"
        echo "${status} ${field1}" >> ${summary}
        # Create the package structure, move tests and BUILD file
        local package=${field1/github.com\//}
        local bazel_files="$(ls -1 ${test_files} BUILD.bazel 2> /dev/null)"
        if [[ -n "${bazel_files}" ]]; then
          mkdir -p ${package}
          targets="${targets} //${package}/..."
          mv ${bazel_files} ${package}
        else
          echo "*** INTERNAL ERROR: missing tests for ${package}, got [${bazel_files/$'\n'/, }]"
        fi
        test_files=""
      fi
    fi
  done < ${report}
  echo "Test summary:"
  # Dump summary reversed, as go test dumps test first, package later.
  tac ${summary}
  echo "Parsed ${test_count} tests, ${tests_failed} tests failed"
  # If any test failed, show the detailed report.
  # Otherwise, we already shown the summary.
  # Exception: when emitting metrics, dump the full report.
  if (( failed )) || [[ "$@" == *" -emitmetrics"* ]]; then
    if (( failed )); then
      echo "There were ${tests_failed} test failures, full log:"
    else
      echo "Dumping full log as metrics were requested:"
    fi
    cat ${report}
  fi
  # Always generate the junit summary.
  bazel test ${targets} > /dev/null 2>&1 || true
  return ${failed}
}

# Install the latest stable Knative/serving in the current cluster.
function start_latest_knative_serving() {
  header "Starting Knative Serving"
  subheader "Installing Istio"
  echo "Installing Istio CRD from ${KNATIVE_ISTIO_CRD_YAML}"
  kubectl apply -f ${KNATIVE_ISTIO_CRD_YAML} || return 1
  echo "Installing Istio from ${KNATIVE_ISTIO_YAML}"
  kubectl apply -f ${KNATIVE_ISTIO_YAML} || return 1
  wait_until_pods_running istio-system || return 1
  kubectl label namespace default istio-injection=enabled || return 1
  subheader "Installing Knative Serving"
  echo "Installing Serving from ${KNATIVE_SERVING_RELEASE}"
  kubectl apply -f ${KNATIVE_SERVING_RELEASE} || return 1
  wait_until_pods_running knative-serving || return 1
  wait_until_pods_running knative-build || return 1
}

# Install the latest stable Knative/build in the current cluster.
function start_latest_knative_build() {
  header "Starting Knative Build"
  subheader "Installing Istio"
  kubectl apply -f ${KNATIVE_ISTIO_YAML} || return 1
  wait_until_pods_running istio-system || return 1
  subheader "Installing Knative Build"
  kubectl apply -f ${KNATIVE_BUILD_RELEASE} || return 1
  wait_until_pods_running knative-build || return 1
}

# Run a go tool, installing it first if necessary.
# Parameters: $1 - tool package/dir for go get/install.
#             $2 - tool to run.
#             $3..$n - parameters passed to the tool.
function run_go_tool() {
  local tool=$2
  if [[ -z "$(which ${tool})" ]]; then
    local action=get
    [[ $1 =~ ^[\./].* ]] && action=install
    go ${action} $1
  fi
  shift 2
  ${tool} "$@"
}

# Run dep-collector to update licenses.
# Parameters: $1 - output file, relative to repo root dir.
#             $2...$n - directories and files to inspect.
function update_licenses() {
  cd ${REPO_ROOT_DIR} || return 1
  local dst=$1
  shift
  run_go_tool ./vendor/github.com/knative/test-infra/tools/dep-collector dep-collector $@ > ./${dst}
}

# Run dep-collector to check for forbidden liceses.
# Parameters: $1...$n - directories and files to inspect.
function check_licenses() {
  # Fetch the google/licenseclassifier for its license db
  go get -u github.com/google/licenseclassifier
  # Check that we don't have any forbidden licenses in our images.
  run_go_tool ./vendor/github.com/knative/test-infra/tools/dep-collector dep-collector -check $@
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
  # https://github.com/tcort/markdown-link-check
  run_lint_tool markdown-link-check "checking links in markdown files" -q $@
}

# Check format of the given markdown files.
# Parameters: $1..$n - files to inspect
function lint_markdown() {
  # https://github.com/markdownlint/markdownlint
  run_lint_tool mdl "linting markdown files" "-r ~MD013" $@
}

# Return 0 if the given parameter is an integer, otherwise 1
# Parameters: $1 - an integer
function is_int() {
  [[ -n $1 && $1 =~ ^[0-9]+$ ]]
}

# Return 0 if the given parameter is the knative release/nightly gcr, 1
# otherwise
# Parameters: $1 - gcr name, e.g. gcr.io/knative-nightly
function is_protected_gcr() {
  [[ -n $1 && "$1" =~ "^gcr.io/knative-(releases|nightly)/?$" ]]
}

