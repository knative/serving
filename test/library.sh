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

# Useful environment variables
[[ -n "${PROW_JOB_ID}" ]] && IS_PROW=1 || IS_PROW=0
readonly IS_PROW
readonly SERVING_ROOT_DIR="$(dirname $(readlink -f ${BASH_SOURCE}))/.."
readonly OUTPUT_GOBIN="${SERVING_ROOT_DIR}/_output/bin"

# Simple header for logging purposes.
function header() {
  echo "================================================="
  echo ${1^^}
  echo "================================================="
}

# Simple subheader for logging purposes.
function subheader() {
  echo "-------------------------------------------------"
  echo $1
  echo "-------------------------------------------------"
}

# Remove ALL images in the given GCR repository.
# Parameters: $1 - GCR repository.
function delete_gcr_images() {
  for image in $(gcloud --format='value(name)' container images list --repository=$1); do
    echo "Checking ${image} for removal"
    delete_gcr_images ${image}
    for digest in $(gcloud --format='get(digest)' container images list-tags ${image} --limit=99999); do
      local full_image="${image}@${digest}"
      echo "Removing ${full_image}"
      gcloud container images delete -q --force-delete-tags ${full_image}
    done
  done
}

# Waits until all pods are running in the given namespace.
# Parameters: $1 - namespace.
function wait_until_pods_running() {
  echo -n "Waiting until all pods in namespace $1 are up"
  local ns="$1"
  for i in {1..150}; do  # timeout after 5 minutes
    local has_pending="$(kubectl get pods -n $ns -o jsonpath='{.items[*].status.phase}' | grep Pending)"
    local pods="$(kubectl get pods -n $1 2>/dev/null | grep -v NAME)"
    if [[ -n "${pods}" && -z "${has_pending}" ]]; then
      echo -e "\nAll pods are up:"
      kubectl get pods -n $1
      return 0
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: timeout waiting for pods to come up"
  kubectl get pods -n $1
  return 1
}

# Waits until the given service has an external IP address.
# Parameters: $1 - namespace.
# Parameters: $2 - service name.
function wait_until_service_has_external_ip() {
  local ns=$1
  local svc=$2
  echo -n "Waiting until service $svc in namespace $ns has an external IP"
  for i in {1..150}; do  # timeout after 15 minutes
    local ip=$(kubectl get svc -n $ns $svc -o jsonpath="{.status.loadBalancer.ingress[0].ip}")
    if [[ -n "${ip}" ]]; then
      echo -e "\nService $svc.$ns has IP $ip"
      return 0
    fi
    echo -n "."
    sleep 6
  done
  echo -e "\n\nERROR: timeout waiting for service $svc.$ns to have external IP"
  kubectl get pods -n $1
  return 1
}

# Returns the name of the Knative Serving pod of the given app.
# Parameters: $1 - Knative Serving app name.
function get_knative_pod() {
  kubectl get pods -n knative-serving --selector=app=$1 --output=jsonpath="{.items[0].metadata.name}"
}

# Sets the given user as cluster admin.
# Parameters: $1 - user
#             $2 - cluster name
#             $3 - cluster zone
function acquire_cluster_admin_role() {
  # Get the password of the admin and use it, as the service account (or the user)
  # might not have the necessary permission.
  local password=$(gcloud --format="value(masterAuth.password)" \
      container clusters describe $2 --zone=$3)
  kubectl --username=admin --password=$password \
      create clusterrolebinding cluster-admin-binding \
      --clusterrole=cluster-admin \
      --user=$1
}

# Runs a go test and generate a junit summary through bazel.
# Parameters: $1... - parameters to go test
function report_go_test() {
  # Just run regular go tests if not on Prow.
  if (( ! IS_PROW )); then
    go test $@
    return
  fi
  local report=$(mktemp)
  local summary=$(mktemp)
  local failed=0
  # Run tests in verbose mode to capture details.
  # go doesn't like repeating -v, so remove if passed.
  local args=("${@/-v}")
  go test -race -v ${args[@]} > ${report} || failed=$?
  # Tests didn't run.
  [[ ! -s ${report} ]] && return 1
  # Create WORKSPACE file, required to use bazel
  touch WORKSPACE
  local targets=""
  # Parse the report and generate fake tests for each passing/failing test.
  while read line ; do
    local fields=(`echo -n ${line}`)
    local field0="${fields[0]}"
    local field1="${fields[1]}"
    local name=${fields[2]}
    # Ignore subtests (those containing slashes)
    if [[ -n "${name##*/*}" ]]; then
      if [[ ${field1} == PASS: || ${field1} == FAIL: ]]; then
        # Populate BUILD.bazel
        local src="${name}.sh"
        echo "exit 0" > ${src}
        if [[ ${field1} == "FAIL:" ]]; then
          read error
          echo "cat <<ERROR-EOF" > ${src}
          echo "${error}" >> ${src}
          echo "ERROR-EOF" >> ${src}
          echo "exit 1" >> ${src}
        fi
        chmod +x ${src}
        echo "sh_test(name=\"${name}\", srcs=[\"${src}\"])" >> BUILD.bazel
      elif [[ ${field0} == FAIL || ${field0} == ok ]]; then
        # Update the summary with the result for the package
        echo "${line}" >> ${summary}
        # Create the package structure, move tests and BUILD file
        local package=${field1/github.com\//}
        mkdir -p ${package}
        targets="${targets} //${package}/..."
        mv *.sh BUILD.bazel ${package}
      fi
    fi
  done < ${report}
  # If any test failed, show the detailed report.
  # Otherwise, just show the summary.
  # Exception: when emitting metrics, dump the full report.
  if (( failed )) || [[ "$@" == *" -emitmetrics"* ]]; then
    cat ${report}
  else
    cat ${summary}
  fi
  # Always generate the junit summary.
  bazel test ${targets} > /dev/null 2>&1
  return ${failed}
}
