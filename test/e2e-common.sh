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
# shellcheck disable=SC1090
source "$(dirname "${BASH_SOURCE[0]}")/../vendor/knative.dev/hack/e2e-tests.sh"
source "$(dirname "${BASH_SOURCE[0]}")/e2e-networking-library.sh"

CERT_MANAGER_VERSION="latest"
# Since default is istio, make default ingress as istio
INGRESS_CLASS=${INGRESS_CLASS:-istio.ingress.networking.knative.dev}
ISTIO_VERSION=""
GLOO_VERSION=""
KOURIER_VERSION=""
AMBASSADOR_VERSION=""
CONTOUR_VERSION=""
CERTIFICATE_CLASS=""
# Only build linux/amd64 bit images
KO_FLAGS="--platform=linux/amd64"

HTTPS=0
export MESH=0

# List of custom YAMLs to install, if specified (space-separated).
INSTALL_CUSTOM_YAMLS=""

UNINSTALL_LIST=()
export TMP_DIR
TMP_DIR="${TMP_DIR:-$(mktemp -d -t ci-$(date +%Y-%m-%d-%H-%M-%S)-XXXXXXXXXX)}"
readonly KNATIVE_DEFAULT_NAMESPACE="knative-serving"
# This the namespace used to install Knative Serving. Use generated UUID as namespace.
export SYSTEM_NAMESPACE
SYSTEM_NAMESPACE="${SYSTEM_NAMESPACE:-$(uuidgen | tr 'A-Z' 'a-z')}"


# Keep this in sync with test/ha/ha.go
readonly REPLICAS=3
readonly BUCKETS=10
HA_COMPONENTS=()

# Latest serving release. If user does not supply this as a flag, the latest
# tagged release on the current branch will be used.
LATEST_SERVING_RELEASE_VERSION=$(latest_version)

# Latest net-istio release.
LATEST_NET_ISTIO_RELEASE_VERSION=$(
  curl -L --silent "https://api.github.com/repos/knative/net-istio/releases" | grep '"tag_name"' \
    | cut -f2 -d: | sed "s/[^v0-9.]//g" | sort | tail -n1)

# Parse our custom flags.
function parse_flags() {
  case "$1" in
    --istio-version)
      [[ $2 =~ ^(stable|latest|head)$ ]] || abort "version format must be 'stable', 'latest', or 'head'"
      readonly ISTIO_VERSION=$2
      readonly INGRESS_CLASS="istio.ingress.networking.knative.dev"
      return 2
      ;;
    --version)
      [[ $2 =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || abort "version format must be 'v[0-9].[0-9].[0-9]'"
      LATEST_SERVING_RELEASE_VERSION=$2
      return 2
      ;;
    --cert-manager-version)
      [[ $2 =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || abort "version format must be '[0-9].[0-9].[0-9]'"
      readonly CERT_MANAGER_VERSION=$2
      readonly CERTIFICATE_CLASS="cert-manager.certificate.networking.knative.dev"
      return 2
      ;;
    --mesh)
      readonly MESH=1
      return 1
      ;;
    --no-mesh)
      readonly MESH=0
      return 1
      ;;
    --https)
      readonly HTTPS=1
      return 1
      ;;
    --custom-yamls)
      [[ -z "$2" ]] && fail_test "Missing argument to --custom-yamls"
      # Expect a list of comma-separated YAMLs.
      INSTALL_CUSTOM_YAMLS="${2//,/ }"
      readonly INSTALL_CUSTOM_YAMLS
      return 2
      ;;
    --gloo-version)
      # currently, the value of --gloo-version is ignored
      # latest version of Gloo pinned in third_party will be installed
      readonly GLOO_VERSION=$2
      readonly INGRESS_CLASS="gloo.ingress.networking.knative.dev"
      return 2
      ;;
    --kourier-version)
      # currently, the value of --kourier-version is ignored
      # latest version of Kourier pinned in third_party will be installed
      readonly KOURIER_VERSION=$2
      readonly INGRESS_CLASS="kourier.ingress.networking.knative.dev"
      return 2
      ;;
    --ambassador-version)
      # currently, the value of --ambassador-version is ignored
      # latest version of Ambassador pinned in third_party will be installed
      readonly AMBASSADOR_VERSION=$2
      readonly INGRESS_CLASS="ambassador.ingress.networking.knative.dev"
      return 2
      ;;
    --contour-version)
      # currently, the value of --contour-version is ignored
      # latest version of Contour pinned in third_party will be installed
      readonly CONTOUR_VERSION=$2
      readonly INGRESS_CLASS="contour.ingress.networking.knative.dev"
      return 2
      ;;
    --kong-version)
      # currently, the value of --kong-version is ignored
      # latest version of Kong pinned in third_party will be installed
      readonly KONG_VERSION=$2
      readonly INGRESS_CLASS="kong"
      return 2
      ;;
    --system-namespace)
      [[ -z "$2" ]] || [[ $2 = --* ]] && fail_test "Missing argument to --system-namespace"
      export SYSTEM_NAMESPACE=$2
      return 2
      ;;
  esac
  return 0
}

# Create all manifests required to install Knative Serving.
# This will build everything from the current source.
# All generated YAMLs will be available and pointed by the corresponding
# environment variables as set in /hack/generate-yamls.sh.
function build_knative_from_source() {
  local FULL_OUTPUT YAML_LIST LOG_OUTPUT ENV_OUTPUT
  YAML_LIST="$(mktemp)"

  # Generate manifests, capture environment variables pointing to the YAML files.
  FULL_OUTPUT="$( \
      source "$(dirname "${BASH_SOURCE[0]}")/../hack/generate-yamls.sh" "${REPO_ROOT_DIR}" "${YAML_LIST}" ; \
      set | grep _YAML=/)"
  LOG_OUTPUT="$(echo "${FULL_OUTPUT}" | grep -v _YAML=/)"
  ENV_OUTPUT="$(echo "${FULL_OUTPUT}" | grep '^[_0-9A-Z]\+_YAML=/')"
  [[ -z "${LOG_OUTPUT}" || -z "${ENV_OUTPUT}" ]] && fail_test "Error generating manifests"
  # Only import the environment variables pointing to the YAML files.
  echo "${LOG_OUTPUT}"
  echo -e "Generated manifests:\n${ENV_OUTPUT}"
  eval "${ENV_OUTPUT}"
}

# Installs Knative Serving in the current cluster.
# If no parameters are passed, installs the current source-based build, unless custom
# YAML files were passed using the --custom-yamls flag.
# Parameters: $1 - Knative Serving version "HEAD" or "latest-release". Default is "HEAD".
#             $2 - Knative Monitoring YAML file (optional)
function install_knative_serving() {
  local version=${1:-"HEAD"}
  if [[ -z "${INSTALL_CUSTOM_YAMLS}" ]]; then
    install_knative_serving_standard "$version" "${2:-}"
    return
  fi
  echo ">> Installing Knative serving from custom YAMLs"
  echo "Custom YAML files: ${INSTALL_CUSTOM_YAMLS}"
  for yaml in ${INSTALL_CUSTOM_YAMLS}; do
    local YAML_NAME=${TMP_DIR}/${yaml##*/}
    sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${yaml} > ${YAML_NAME}
    echo "Installing '${YAML_NAME}'"
    kubectl create -f "${YAML_NAME}" || return 1
  done
}

# Installs Knative Serving in the current cluster.
# If no parameters are passed, installs the current source-based build.
# Parameters: $1 - Knative Serving version "HEAD" or "latest-release".
#             $2 - Knative Monitoring YAML file (optional)
function install_knative_serving_standard() {
  echo ">> Creating ${SYSTEM_NAMESPACE} namespace if it does not exist"
  kubectl get ns ${SYSTEM_NAMESPACE} || kubectl create namespace ${SYSTEM_NAMESPACE}
  if (( MESH )); then
    kubectl label namespace ${SYSTEM_NAMESPACE} istio-injection=enabled
  fi
  # Delete the test namespace
  add_trap "kubectl delete namespace ${SYSTEM_NAMESPACE} --ignore-not-found=true" SIGKILL SIGTERM SIGQUIT

  echo ">> Installing Knative CRD"
  SERVING_RELEASE_YAML=""
  SERVING_POST_INSTALL_JOBS_YAML=""
  if [[ "$1" == "HEAD" ]]; then
    # If we need to build from source, then kick that off first.
    build_knative_from_source

    echo "CRD YAML: ${SERVING_CRD_YAML}"
    kubectl apply -f "${SERVING_CRD_YAML}" || return 1
    UNINSTALL_LIST+=( "${SERVING_CRD_YAML}" )

    echo "DOMAIN MAPPING CRD YAML: ${SERVING_DOMAINMAPPING_CRD_YAML}"
    kubectl apply -f "${SERVING_DOMAINMAPPING_CRD_YAML}" || return 1
    UNINSTALL_LIST+=( "${SERVING_DOMAINMAPPING_CRD_YAML}" )
  else
    # Download the latest release of Knative Serving.
    local url="https://github.com/knative/serving/releases/download/${LATEST_SERVING_RELEASE_VERSION}"

    local SERVING_RELEASE_YAML=${TMP_DIR}/"serving-${LATEST_SERVING_RELEASE_VERSION}.yaml"
    local SERVING_POST_INSTALL_JOBS_YAML=${TMP_DIR}/"serving-${LATEST_SERVING_RELEASE_VERSION}-post-install-jobs.yaml"

    wget "${url}/serving-crds.yaml" -O "${SERVING_RELEASE_YAML}" \
      || fail_test "Unable to download latest knative/serving CRD file."
    wget "${url}/serving-core.yaml" -O ->> "${SERVING_RELEASE_YAML}" \
      || fail_test "Unable to download latest knative/serving core file."
    # TODO - switch to upgrade yaml (SERVING_POST_INSTALL_JOBS_YAML) after 0.16 is released
    wget "${url}/serving-storage-version-migration.yaml" -O "${SERVING_POST_INSTALL_JOBS_YAML}" \
      || fail_test "Unable to download latest knative/serving post install file."

    # Replace the default system namespace with the test's system namespace.
    sed -i "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${SERVING_RELEASE_YAML}
    sed -i "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${SERVING_POST_INSTALL_JOBS_YAML}

    echo "Knative YAML: ${SERVING_RELEASE_YAML}"
    ko apply -f "${SERVING_RELEASE_YAML}" --selector=knative.dev/crd-install=true || return 1
  fi

  if [[ -z "${REUSE_INGRESS:-}" ]]; then
    echo ">> Installing Ingress"
    if [[ -n "${GLOO_VERSION:-}" ]]; then
      install_gloo || return 1
    elif [[ -n "${KOURIER_VERSION:-}" ]]; then
      install_kourier || return 1
    elif [[ -n "${AMBASSADOR_VERSION:-}" ]]; then
      install_ambassador || return 1
    elif [[ -n "${CONTOUR_VERSION:-}" ]]; then
      install_contour || return 1
    elif [[ -n "${KONG_VERSION:-}" ]]; then
      install_kong || return 1
    else
      if [[ "$1" == "HEAD" ]]; then
        install_istio "./third_party/istio-latest/net-istio.yaml" || return 1
      else
        # Download the latest release of net-istio.
        local url="https://github.com/knative/net-istio/releases/download/${LATEST_NET_ISTIO_RELEASE_VERSION}"
        local yaml="net-istio.yaml"
        local YAML_NAME=${TMP_DIR}/"net-istio-${LATEST_NET_ISTIO_RELEASE_VERSION}.yaml"
        wget "${url}/${yaml}" -O "${YAML_NAME}" \
          || fail_test "Unable to download latest knative/net-istio release."
        echo "net-istio YAML: ${YAML_NAME}"
        install_istio $YAML_NAME || return 1
      fi
    fi
  fi

  echo ">> Installing Cert-Manager"
  readonly INSTALL_CERT_MANAGER_YAML="./third_party/cert-manager-${CERT_MANAGER_VERSION}/cert-manager.yaml"
  echo "Cert Manager YAML: ${INSTALL_CERT_MANAGER_YAML}"
  # We skip installing cert-manager if it has been installed as "kubectl apply" will be stuck when
  # cert-manager has been installed. https://github.com/jetstack/cert-manager/issues/3367
  kubectl get ns cert-manager || kubectl apply -f "${INSTALL_CERT_MANAGER_YAML}" --validate=false || return 1
  UNINSTALL_LIST+=( "${INSTALL_CERT_MANAGER_YAML}" )
  readonly NET_CERTMANAGER_YAML="./third_party/cert-manager-${CERT_MANAGER_VERSION}/net-certmanager.yaml"
  echo "net-certmanager YAML: ${NET_CERTMANAGER_YAML}"
  local CERT_YAML_NAME=${TMP_DIR}/${NET_CERTMANAGER_YAML##*/}
  sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${NET_CERTMANAGER_YAML} > ${CERT_YAML_NAME}
  kubectl apply \
      -f "${CERT_YAML_NAME}" || return 1
  UNINSTALL_LIST+=( "${CERT_YAML_NAME}" )

  echo ">> Installing Knative serving"
  HA_COMPONENTS+=( "controller" "webhook" "autoscaler-hpa" "autoscaler" "domainmapping-webhook" )
  if [[ "$1" == "HEAD" ]]; then
    local CORE_YAML_NAME=${TMP_DIR}/${SERVING_CORE_YAML##*/}
    sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${SERVING_CORE_YAML} > ${CORE_YAML_NAME}
    local HPA_YAML_NAME=${TMP_DIR}/${SERVING_HPA_YAML##*/}
    sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${SERVING_HPA_YAML} > ${HPA_YAML_NAME}
    local DOMAINMAPPING_YAML_NAME=${TMP_DIR}/${SERVING_DOMAINMAPPING_YAML##*/}
    sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${SERVING_DOMAINMAPPING_YAML} > ${DOMAINMAPPING_YAML_NAME}
    local POST_INSTALL_JOBS_YAML_NAME=${TMP_DIR}/${SERVING_POST_INSTALL_JOBS_YAML##*/}
    sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${SERVING_POST_INSTALL_JOBS_YAML} > ${POST_INSTALL_JOBS_YAML_NAME}

    echo "Knative YAML: ${CORE_YAML_NAME} and ${HPA_YAML_NAME} and ${DOMAINMAPPING_YAML_NAME}"
    kubectl apply \
	    -f "${CORE_YAML_NAME}" \
	    -f "${DOMAINMAPPING_YAML_NAME}" \
	    -f "${HPA_YAML_NAME}" || return 1
    UNINSTALL_LIST+=( "${CORE_YAML_NAME}" "${HPA_YAML_NAME}" "${DOMAINMAPPING_YAML_NAME}" )
    kubectl create -f ${POST_INSTALL_JOBS_YAML_NAME}
  else
    echo "Knative YAML: ${SERVING_RELEASE_YAML}"
    # We use ko because it has better filtering support for CRDs.
    ko apply -f "${SERVING_RELEASE_YAML}" || return 1
    ko create -f "${SERVING_POST_INSTALL_JOBS_YAML}" || return 1
    UNINSTALL_LIST+=( "${SERVING_RELEASE_YAML}" )
  fi

  echo ">> Configuring the default Ingress: ${INGRESS_CLASS}"
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-network
  namespace: ${SYSTEM_NAMESPACE}
  labels:
    serving.knative.dev/release: devel
data:
  ingress.class: ${INGRESS_CLASS}
EOF

  echo ">> Turning on profiling.enable"
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-observability
  namespace: ${SYSTEM_NAMESPACE}
data:
  profiling.enable: "true"
EOF

  echo ">> Patching activator HPA"
  # We set min replicas to 15 for testing multiple activator pods.
  kubectl -n ${SYSTEM_NAMESPACE} patch hpa activator --patch '{"spec":{"minReplicas":15}}' || return 1
}

# Check if we should use --resolvabledomain.  In case the ingress only has
# hostname, we doesn't yet have a way to support resolvable domain in tests.
function use_resolvable_domain() {
  # Temporarily turning off xip.io tests, as DNS errors aren't always retried.
  echo "false"
}

# Check if we should specify --ingressClass
function ingress_class() {
  if [[ -z "${INGRESS_CLASS}" ]]; then
    echo ""
  else
    echo "--ingressClass=${INGRESS_CLASS}"
  fi
}

# Check if we should specify --certificateClass
function certificate_class() {
  if [[ -z "${CERTIFICATE_CLASS}" ]]; then
    echo ""
  else
    echo "--certificateClass=${CERTIFICATE_CLASS}"
  fi
}

# Uninstalls Knative Serving from the current cluster.
function knative_teardown() {
  if [[ -z "${INSTALL_CUSTOM_YAMLS}" && -z "${UNINSTALL_LIST[@]}" ]]; then
    echo "install_knative_serving() was not called, nothing to uninstall"
    return 0
  fi
  if [[ -n "${INSTALL_CUSTOM_YAMLS}" ]]; then
    echo ">> Uninstalling Knative serving from custom YAMLs"
    for yaml in ${INSTALL_CUSTOM_YAMLS}; do
      echo "Uninstalling '${yaml}'"
      kubectl delete --ignore-not-found=true -f "${yaml}" || return 1
    done
  else
    echo ">> Uninstalling Knative serving"
    for i in ${!UNINSTALL_LIST[@]}; do
	# We uninstall elements in the reverse of the order they were installed.
	local YAML="${UNINSTALL_LIST[$(( ${#array[@]} - $i ))]}"
	echo ">> Bringing down YAML: ${YAML}"
	kubectl delete --ignore-not-found=true -f "${YAML}" || return 1
    done
  fi
}

# Create test resources and images
function test_setup() {
  echo ">> Replacing ${KNATIVE_DEFAULT_NAMESPACE} with the actual namespace for Knative Serving..."
  local TEST_DIR=${TMP_DIR}/test
  mkdir -p ${TEST_DIR}
  cp -r test/* ${TEST_DIR}
  find ${TEST_DIR} -type f -name "*.yaml" -exec sed -i "s/${KNATIVE_DEFAULT_NAMESPACE}/${SYSTEM_NAMESPACE}/g" {} +

  echo ">> Setting up logging..."

  # Install kail if needed.
  if ! which kail > /dev/null; then
    bash <( curl -sfL https://raw.githubusercontent.com/boz/kail/master/godownloader.sh) -b "$GOPATH/bin"
  fi

  # Capture all logs.
  kail > "${ARTIFACTS}/k8s.log-$(basename "${E2E_SCRIPT}").txt" &
  local kail_pid=$!
  # Clean up kail so it doesn't interfere with job shutting down
  add_trap "kill $kail_pid || true" EXIT

  # Capture lease changes
  kubectl get lease -A -w -o yaml > ${ARTIFACTS}/leases-$(basename ${E2E_SCRIPT}).log &
  local leases_pid=$!
  # Clean up the lease logging so it doesn't interfere with job shutting down
  add_trap "kill $leases_pid || true" EXIT

  echo ">> Waiting for Serving components to be running..."
  wait_until_pods_running ${SYSTEM_NAMESPACE} || return 1

  local TEST_CONFIG_DIR=${TEST_DIR}/config
  echo ">> Creating test resources (${TEST_CONFIG_DIR}/)"
  ko apply ${KO_FLAGS} -f ${TEST_CONFIG_DIR}/ || return 1
  if (( MESH )); then
    kubectl label namespace serving-tests istio-injection=enabled
    kubectl label namespace serving-tests-alt istio-injection=enabled
  fi

  echo ">> Uploading test images..."
  ${REPO_ROOT_DIR}/test/upload-test-images.sh || return 1

  echo ">> Waiting for Cert Manager components to be running..."
  wait_until_pods_running cert-manager || return 1

  echo ">> Waiting for Ingress provider to be running..."
  wait_until_ingress_running || return 1
}

# Apply the logging config for testing. This should be called after test_setup has been triggered.
function test_logging_config_setup() {
  echo ">> Setting up test logging config..."
  ko apply ${KO_FLAGS} -f ${TMP_DIR}/test/config/config-logging.yaml || return 1
}

# Delete test resources
function test_teardown() {
  local TEST_CONFIG_DIR=${TMP_DIR}/test/config
  echo ">> Removing test resources (${TEST_CONFIG_DIR}/)"
  ko delete --ignore-not-found=true --now -f ${TEST_CONFIG_DIR}/

  echo ">> Ensuring test namespaces are clean"
  kubectl delete all --all --ignore-not-found --now --timeout 60s -n serving-tests
  kubectl delete --ignore-not-found --now --timeout 60s namespace serving-tests
  kubectl delete all --all --ignore-not-found --now --timeout 60s -n serving-tests-alt
  kubectl delete --ignore-not-found --now --timeout 60s namespace serving-tests-alt
}

# Dump more information when test fails.
function dump_extra_cluster_state() {
  echo ">>> Routes:"
  kubectl get routes -o yaml --all-namespaces
  echo ">>> Configurations:"
  kubectl get configurations -o yaml --all-namespaces
  echo ">>> Revisions:"
  kubectl get revisions -o yaml --all-namespaces
  echo ">>> PodAutoscalers:"
  kubectl get podautoscalers -o yaml --all-namespaces
  echo ">>> SKSs:"
  kubectl get serverlessservices -o yaml --all-namespaces
}

function wait_for_leader_controller() {
  echo -n "Waiting for a leader Controller"
  for i in {1..150}; do  # timeout after 5 minutes
    local leader=$(kubectl get lease -n "${SYSTEM_NAMESPACE}" -ojsonpath='{range .items[*].spec}{"\n"}{.holderIdentity}' | cut -d"_" -f1 | grep "^controller-" | head -1)
    # Make sure the leader pod exists.
    if [ -n "${leader}" ] && kubectl get pod "${leader}" -n "${SYSTEM_NAMESPACE}"  >/dev/null 2>&1; then
      echo -e "\nNew leader Controller has been elected"
      return 0
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: timeout waiting for leader controller"
  return 1
}

function toggle_feature() {
  local FEATURE="$1"
  local STATE="$2"
  local CONFIG="${3:-config-features}"
  echo -n "Setting feature ${FEATURE} to ${STATE}"
  kubectl patch cm "${CONFIG}" -n "${SYSTEM_NAMESPACE}" -p '{"data":{"'${FEATURE}'":"'${STATE}'"}}'
  # We don't have a good mechanism for positive handoff so sleep :(
  echo "Waiting 30s for change to get picked up."
  sleep 30
}

function immediate_gc() {
  echo -n "Setting config-gc to immediate garbage collection"
  local DATA='{"data":{'`
      `'"retain-since-create-time":"disabled",'`
      `'"retain-since-last-active-time":"disabled",'`
      `'"min-non-active-revisions":"0",'`
      `'"max-non-active-revisions":"0"'`
      `"}}"
  kubectl patch cm "config-gc" -n "${SYSTEM_NAMESPACE}" -p "${DATA}"
  echo "Waiting 30s for change to get picked up."
  sleep 30
}

function scale_controlplane() {
  for deployment in "$@"; do
    # Make sure all pods run in leader-elected mode.
    kubectl -n "${SYSTEM_NAMESPACE}" scale deployment "$deployment" --replicas=0 || fail_test
    # Give it time to kill the pods.
    sleep 5
    # Scale up components for HA tests
    kubectl -n "${SYSTEM_NAMESPACE}" scale deployment "$deployment" --replicas="${REPLICAS}" || fail_test
  done
}

function disable_chaosduck() {
  kubectl -n "${SYSTEM_NAMESPACE}" scale deployment "chaosduck" --replicas=0 || fail_test
}

function enable_chaosduck() {
  kubectl -n "${SYSTEM_NAMESPACE}" scale deployment "chaosduck" --replicas=1 || fail_test
}

function install_latest_release() {
  header "Installing Knative latest public release"

  install_knative_serving latest-release \
      || fail_test "Knative latest release installation failed"
  test_logging_config_setup

  wait_until_pods_running ${SYSTEM_NAMESPACE}
  wait_until_batch_job_complete ${SYSTEM_NAMESPACE}
}

function install_head_reuse_ingress() {
  header "Installing Knative head release and reusing ingress"
  # Keep the existing ingress and do not upgrade it. The ingress upgrade
  # makes ongoing requests fail.
  REUSE_INGRESS=true install_knative_serving || fail_test "Knative head release installation failed"
  test_logging_config_setup

  wait_until_pods_running ${SYSTEM_NAMESPACE}
  wait_until_batch_job_complete ${SYSTEM_NAMESPACE}
}

function knative_setup() {
  install_latest_release
}
