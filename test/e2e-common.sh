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

# Temporarily increasing the cluster size for serving tests to rule out
# resource / eviction as causes of flakiness.  These env vars are consumed
# in the test-infra/scripts/e2e-tests.sh.
E2E_MIN_CLUSTER_NODES=4
E2E_MAX_CLUSTER_NODES=4

# This script provides helper methods to perform cluster actions.
source $(dirname $0)/../vendor/github.com/knative/test-infra/scripts/e2e-tests.sh

# Current YAMLs used to install Knative Serving.
INSTALL_ISTIO_CRD_YAML=""
INSTALL_ISTIO_YAML=""
# TODO(#2122): Install monitoring as well once we have e2e testing for it.
INSTALL_RELEASE_YAML=""

# Build is used by some tests and so is also included here.
readonly INSTALL_BUILD_DIR="./third_party/config/build/"
readonly INSTALL_PIPELINE_DIR="./third_party/config/pipeline/"

# List of custom YAMLs to install, if specified (space-separated).
INSTALL_CUSTOM_YAMLS=""

# Parse our custom flags.
function parse_flags() {
  if [[ "$1" == "--custom-yamls" ]]; then
    [[ -z "$2" ]] && fail_test "Missing argument to --custom-yamls"
    # Expect a list of comma-separated YAMLs.
    INSTALL_CUSTOM_YAMLS="${2//,/ }"
    readonly INSTALL_CUSTOM_YAMLS
    return 2
  fi
  return 0
}

# Create all manifests required to install Knative Serving.
# This will build everything from the current source.
# All generated YAMLs will be available and pointed by the corresponding
# environment variables as set in /hack/generate-yamls.sh.
function build_knative_from_source() {
  local YAML_LIST="$(mktemp)"
  # Generate manifests, capture environment variables pointing to the YAML files.
  local FULL_OUTPUT="$( \
      source $(dirname $0)/../hack/generate-yamls.sh ${REPO_ROOT_DIR} ${YAML_LIST} ; \
      set | grep _YAML=/)"
  local LOG_OUTPUT="$(echo "${FULL_OUTPUT}" | grep -v _YAML=/)"
  local ENV_OUTPUT="$(echo "${FULL_OUTPUT}" | grep '^[_0-9A-Z]\+_YAML=/')"
  [[ -z "${LOG_OUTPUT}" || -z "${ENV_OUTPUT}" ]] && fail_test "Error generating manifests"
  # Only import the environment variables pointing to the YAML files.
  echo "${LOG_OUTPUT}"
  echo -e "Generated manifests:\n${ENV_OUTPUT}"
  eval "${ENV_OUTPUT}"
}

# Installs Knative Serving in the current cluster, and waits for it to be ready.
# If no parameters are passed, installs the current source-based build, unless custom
# YAML files were passed using the --custom-yamls flag.
# Parameters: $1 - Istio CRD YAML file
#             $2 - Istio YAML file
#             $3 - Knative Serving YAML file
function install_knative_serving() {
  if [[ -z "${INSTALL_CUSTOM_YAMLS}" ]]; then
    install_knative_serving_standard "$1" "$2" "$3"
    return
  fi
  echo ">> Installing Knative serving from custom YAMLs"
  kubectl label namespace default istio-injection=enabled
  echo "Custom YAML files: ${INSTALL_CUSTOM_YAMLS}"
  for yaml in ${INSTALL_CUSTOM_YAMLS}; do
    echo "Installing '${yaml}'"
    kubectl create -f "${yaml}" || return 1
  done
  echo ">> Creating test resources (test/config/)"
  ko apply -f test/config/ || return 1

  wait_until_pods_running knative-serving || return 1
}

# Installs Knative Serving in the current cluster, and waits for it to be ready.
# If no parameters are passed, installs the current source-based build.
# Parameters: $1 - Istio CRD YAML file
#             $2 - Istio YAML file
#             $3 - Knative Serving YAML file
function install_knative_serving_standard() {
  INSTALL_ISTIO_CRD_YAML=$1
  INSTALL_ISTIO_YAML=$2
  INSTALL_RELEASE_YAML=$3
  if [[ -z "${INSTALL_ISTIO_CRD_YAML}" ]]; then
    build_knative_from_source
    INSTALL_ISTIO_CRD_YAML="${ISTIO_CRD_YAML}"
    INSTALL_ISTIO_YAML="${ISTIO_YAML}"
    # TODO(#2122): Install monitoring as well once we have e2e testing for it.
    INSTALL_RELEASE_YAML="${SERVING_YAML}"
  fi

  echo ">> Installing Knative serving"
  echo "Istio CRD YAML: ${INSTALL_ISTIO_CRD_YAML}"
  echo "Istio YAML: ${INSTALL_ISTIO_YAML}"
  echo "Knative YAML: ${INSTALL_RELEASE_YAML}"
  echo "Knative Build YAML: ${INSTALL_BUILD_DIR}"
  echo "Knative Build Pipeline YAML: ${INSTALL_PIPELINE_DIR}"

  echo ">> Bringing up Istio"
  kubectl apply -f "${INSTALL_ISTIO_CRD_YAML}" || return 1
  kubectl apply -f "${INSTALL_ISTIO_YAML}" || return 1

  echo ">> Installing Build"
  # TODO: should this use a released copy of Build?
  kubectl apply -f "${INSTALL_BUILD_DIR}" || return 1
  kubectl apply -f "${INSTALL_PIPELINE_DIR}" || return 1

  echo ">> Bringing up Serving"
  kubectl apply -f "${INSTALL_RELEASE_YAML}" || return 1

  echo ">> Adding more activator pods."
  # This command would fail if the HPA already exist, like during upgrade test.
  # Therefore we don't exit on failure, and don't log an error message.
  kubectl autoscale deploy --min=2 --max=2 -n knative-serving activator 2>/dev/null

  # Due to the lack of Status in Istio, we have to ignore failures in initial requests.
  #
  # However, since network configurations may reach different ingress pods at slightly
  # different time, even ignoring failures for initial requests won't ensure subsequent
  # requests will succeed all the time.  We are disabling ingress pod autoscaling here
  # to avoid having too much flakes in the tests.  That would allow us to be stricter
  # when checking non-probe requests to discover other routing issues.
  #
  # To compensate for this scaling down, we increase the CPU request for these pods.
  #
  # We should revisit this when Istio API exposes a Status that we can rely on.
  # TODO(tcnghia): remove this when https://github.com/istio/istio/issues/882 is fixed.
  echo ">> Patching Istio"
  # There are reports of Envoy failing (503) when istio-pilot is overloaded.
  # We generously add more pilot instances here to verify if we can reduce flakes.
  if kubectl get hpa -n istio-system istio-pilot 2>/dev/null; then
    # If HPA exists, update it.  Since patching will return non-zero if no change
    # is made, we don't return on failure here.
    kubectl patch hpa -n istio-system istio-pilot \
      --patch '{"spec": {"minReplicas": 3, "maxReplicas": 10, "targetCPUUtilizationPercentage": 60}}' \
      `# Ignore error messages to avoid causing red herrings in the tests` \
      2>/dev/null
  else
    # Some versions of Istio don't provide an HPA for pilot.
    kubectl autoscale -n istio-system deploy istio-pilot --min=3 --max=10 --cpu-percent=60 || return 1
  fi

  echo ">> Creating test resources (test/config/)"
  ko apply -f test/config/ || return 1

  wait_until_pods_running knative-serving || return 1
  wait_until_pods_running istio-system || return 1
  wait_until_service_has_external_ip istio-system istio-ingressgateway
}

# Uninstalls Knative Serving from the current cluster.
function uninstall_knative_serving() {
  if [[ -z "${INSTALL_CUSTOM_YAMLS}" && -z "${INSTALL_RELEASE_YAML}" ]]; then
    echo "install_knative_serving() was not called, nothing to uninstall"
    return 0
  fi
  echo ">> Removing test resources (test/config/)"
  ko delete --ignore-not-found=true -f test/config/ || return 1
  if [[ -n "${INSTALL_CUSTOM_YAMLS}" ]]; then
    echo ">> Uninstalling Knative serving from custom YAMLs"
    for yaml in ${INSTALL_CUSTOM_YAMLS}; do
      echo "Uninstalling '${yaml}'"
      kubectl delete --ignore-not-found=true -f "${yaml}" || return 1
    done
  else
    echo ">> Uninstalling Knative serving"
    echo "Istio YAML: ${INSTALL_ISTIO_YAML}"
    echo "Knative YAML: ${INSTALL_RELEASE_YAML}"
    echo "Knative Build YAML: ${INSTALL_BUILD_DIR}"
    echo "Knative Build Pipeline YAML: ${INSTALL_PIPELINE_DIR}"
    echo ">> Bringing down Serving"
    ko delete --ignore-not-found=true -f "${INSTALL_RELEASE_YAML}" || return 1
    echo ">> Bringing down Build"
    ko delete --ignore-not-found=true -f "${INSTALL_BUILD_DIR}" || return 1
    ko delete --ignore-not-found=true -f "${INSTALL_PIPELINE_DIR}" || return 1
    echo ">> Bringing down Istio"
    kubectl delete --ignore-not-found=true -f "${INSTALL_ISTIO_YAML}" || return 1
    kubectl delete --ignore-not-found=true clusterrolebinding cluster-admin-binding
  fi
}

# Publish all e2e test images in ${REPO_ROOT_DIR}/test/test_images/
function publish_test_images() {
  echo ">> Creating test namespace"
  kubectl create namespace serving-tests
  ${REPO_ROOT_DIR}/test/upload-test-images.sh
}

# Deletes everything created on the cluster including all knative and istio components.
function teardown() {
  uninstall_knative_serving
}
