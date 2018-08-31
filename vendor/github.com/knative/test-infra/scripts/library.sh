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

# Public images and yaml files.
readonly KNATIVE_ISTIO_YAML=https://storage.googleapis.com/knative-releases/serving/latest/istio.yaml
readonly KNATIVE_SERVING_RELEASE=https://storage.googleapis.com/knative-releases/serving/latest/release.yaml
readonly KNATIVE_BUILD_RELEASE=https://storage.googleapis.com/knative-releases/build/latest/release.yaml
readonly KNATIVE_EVENTING_RELEASE=https://storage.googleapis.com/knative-releases/eventing/latest/release.yaml

# Useful environment variables
[[ -n "${PROW_JOB_ID}" ]] && IS_PROW=1 || IS_PROW=0
readonly IS_PROW
readonly REPO_ROOT_DIR="$(git rev-parse --show-toplevel)"

# Display a box banner.
# Parameters: $1 - character to use for the box.
#             $2 - banner message.
function make_banner() {
    local msg="$1$1$1$1 $2 $1$1$1$1"
    local border="${msg//[-0-9A-Za-z _.,]/$1}"
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
    kubectl ${KUBECTL_ARGS} 2>&1 > /dev/null || return 0
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

# Returns the name of the pod of the given app.
# Parameters: $1 - app name.
#             $2 - namespace (optional).
function get_app_pod() {
  local namespace=""
  [[ -n $2 ]] && namespace="-n $2"
  kubectl get pods ${namespace} --selector=app=$1 --output=jsonpath="{.items[0].metadata.name}"
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
  kubectl config set-credentials cluster-admin \
      --username=admin --password=${password}
  kubectl config set-context $(kubectl config current-context) \
      --user=cluster-admin
  kubectl create clusterrolebinding cluster-admin-binding \
      --clusterrole=cluster-admin \
      --user=$1
  # Reset back to the default account
  gcloud container clusters get-credentials \
      $2 --zone=$3 --project $(gcloud config get-value project)
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

# Install the latest stable Knative/serving in the current cluster.
function start_latest_knative_serving() {
  header "Starting Knative Serving"
  subheader "Installing Istio"
  kubectl apply -f ${KNATIVE_ISTIO_YAML} || return 1
  wait_until_pods_running istio-system || return 1
  kubectl label namespace default istio-injection=enabled || return 1
  subheader "Installing Knative Serving"
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

# Run dep-collector, installing it first if necessary.
# Parameters: $1..$n - parameters passed to dep-collector.
function run_dep_collector() {
  local local_dep_collector="$(which dep-collector)"
  if [[ -z ${local_dep_collector} ]]; then
    go get -u github.com/mattmoor/dep-collector
  fi
  dep-collector $@
}

# Run dep-collector to update licenses.
# Parameters: $1 - output file, relative to repo root dir.
#             $2...$n - directories and files to inspect.
function update_licenses() {
  cd ${REPO_ROOT_DIR} || return 1
  local dst=$1
  shift
  run_dep_collector $@ > ./${dst}
}

# Run dep-collector to check for forbidden liceses.
# Parameters: $1...$n - directories and files to inspect.
function check_licenses() {
  # Fetch the google/licenseclassifier for its license db
  go get -u github.com/google/licenseclassifier
  # Check that we don't have any forbidden licenses in our images.
  run_dep_collector -check $@
}

# Check links in all .md files in the repo.
function check_links_in_markdown() {
  local checker="markdown-link-check"
  if ! hash ${checker} 2>/dev/null; then
    warning "${checker} not installed, not checking links in .md files"
    return 0
  fi
  local failed=0
  for md_file in $(find ${REPO_ROOT_DIR} -name \*.md); do
    ${checker} -q ${md_file} || failed=1
  done
  return ${failed}
}
