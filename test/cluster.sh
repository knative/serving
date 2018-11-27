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

# This script provides helper methods to perform cluster actions.

source $(dirname $0)/../vendor/github.com/knative/test-infra/scripts/e2e-tests.sh

# Current YAMLs used to install Knative Serving.
INSTALL_ISTIO_CRD_YAML=""
INSTALL_ISTIO_YAML=""
INSTALL_RELEASE_YAML=""

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
# If no parameters are passed, installs the current source-based build.
# Parameters: $1 - Istio CRD YAML file
#             $2 - Istio YAML file
#             $3 - Knative Serving YAML file
function install_knative_serving() {
  INSTALL_ISTIO_CRD_YAML=$1
  INSTALL_ISTIO_YAML=$2
  INSTALL_RELEASE_YAML=$3
  if [[ -z "${INSTALL_ISTIO_CRD_YAML}" ]]; then
    build_knative_from_source
    INSTALL_ISTIO_CRD_YAML="${ISTIO_CRD_YAML}"
    INSTALL_ISTIO_YAML="${ISTIO_YAML}"
    # TODO(#2122): Use RELEASE_YAML once we have monitoring e2e.
    INSTALL_RELEASE_YAML="${RELEASE_NO_MON_YAML}"
  fi
  echo ">> Installing Knative serving"
  echo "Istio CRD YAML: ${INSTALL_ISTIO_CRD_YAML}"
  echo "Istio YAML: ${INSTALL_ISTIO_YAML}"
  echo "Knative YAML: ${INSTALL_RELEASE_YAML}"
  echo ">> Bringing up Istio"
  kubectl apply -f "${INSTALL_ISTIO_CRD_YAML}" || return 1
  kubectl apply -f "${INSTALL_ISTIO_YAML}" || return 1

  echo ">> Bringing up Serving"
  kubectl apply -f "${INSTALL_RELEASE_YAML}" || return 1

  echo ">> Adding more activator pods."
  kubectl scale deploy --replicas=2 -n knative-serving activator || return 1

  # Due to the lack of Status in Istio, we have to ignore failures in initial requests.
  #
  # However, since network configurations may reach different ingress pods at slightly
  # different time, even ignoring failures for initial requests won't ensure subsequent
  # requests will succeed all the time.  We are disabling ingress pod autoscaling here
  # to avoid having too much flakes in the tests.  That would allow us to be stricter
  # when checking non-probe requests to discover other routing issues.
  #
  # We should revisit this when Istio API exposes a Status that we can rely on.
  # TODO(tcnghia): remove this when https://github.com/istio/istio/issues/882 is fixed.
  echo ">> Patching Istio"
  kubectl patch hpa -n istio-system knative-ingressgateway --patch '{"spec": {"maxReplicas": 1}}' || return 1

  echo ">> Creating test resources (test/config/)"
  ko apply -f test/config/ || return 1

  wait_until_pods_running knative-serving || return 1
  wait_until_pods_running istio-system || return 1
  wait_until_service_has_external_ip istio-system knative-ingressgateway
}

# Uninstalls Knative Serving from the current cluster.
function uninstall_knative_serving() {
  if [[ -z "${INSTALL_RELEASE_YAML}" ]]; then
    echo "install_knative_serving() was not called, nothing to uninstall"
    return 0
  fi
  echo ">> Uninstalling Knative serving"
  echo "Istio YAML: ${INSTALL_ISTIO_YAML}"
  echo "Knative YAML: ${INSTALL_RELEASE_YAML}"
  echo ">> Removing test resources (test/config/)"
  ko delete --ignore-not-found=true -f test/config/ || return 1

  echo ">> Bringing down Serving"
  ko delete --ignore-not-found=true -f "${INSTALL_RELEASE_YAML}" || return 1

  echo ">> Bringing down Istio"
  kubectl delete --ignore-not-found=true -f "${INSTALL_ISTIO_YAML}" || return 1
  kubectl delete --ignore-not-found=true clusterrolebinding cluster-admin-binding
}

# Publish all e2e test images in ${REPO_ROOT_DIR}/test/test_images/
function publish_test_images() {
  echo ">> Publishing test images"
  kubectl create namespace serving-tests
  local image_dirs="$(find ${REPO_ROOT_DIR}/test/test_images -mindepth 1 -maxdepth 1 -type d)"
  for image_dir in ${image_dirs}; do
    ko publish -P "github.com/knative/serving/test/test_images/$(basename ${image_dir})" || return 1
  done
}
