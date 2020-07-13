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
# resource/eviction as causes of flakiness. These env vars are consumed
# in the test-infra/scripts/e2e-tests.sh. Use the existing value, if provided
# with the job config.
E2E_MIN_CLUSTER_NODES=${E2E_MIN_CLUSTER_NODES:-4}
E2E_MAX_CLUSTER_NODES=${E2E_MAX_CLUSTER_NODES:-4}
E2E_CLUSTER_MACHINE=${E2E_CLUSTER_MACHINE:-e2-standard-8}

# This script provides helper methods to perform cluster actions.
source $(dirname $0)/../vendor/knative.dev/test-infra/scripts/e2e-tests.sh

CERT_MANAGER_VERSION="0.12.0"
# Since default is istio, make default ingress as istio
INGRESS_CLASS=${INGRESS_CLASS:-istio.ingress.networking.knative.dev}
ISTIO_VERSION=""
GLOO_VERSION=""
KOURIER_VERSION=""
AMBASSADOR_VERSION=""
CONTOUR_VERSION=""
CERTIFICATE_CLASS=""

HTTPS=0
MESH=0
INSTALL_MONITORING=0

# List of custom YAMLs to install, if specified (space-separated).
INSTALL_CUSTOM_YAMLS=""

UNINSTALL_LIST=()
TMP_DIR=$(mktemp -d -t ci-$(date +%Y-%m-%d-%H-%M-%S)-XXXXXXXXXX)
readonly TMP_DIR
readonly KNATIVE_DEFAULT_NAMESPACE="knative-serving"
# This the namespace used to install Knative Serving. Use generated UUID as namespace.
export SYSTEM_NAMESPACE
SYSTEM_NAMESPACE=$(uuidgen | tr 'A-Z' 'a-z')

# Parse our custom flags.
function parse_flags() {
  case "$1" in
    --istio-version)
      [[ $2 =~ ^[0-9]+\.[0-9]+(\.[0-9]+|\-latest)$ ]] || abort "version format must be '[0-9].[0-9].[0-9]' or '[0-9].[0-9]-latest"
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
    --install-monitoring)
      readonly INSTALL_MONITORING=1
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
# Parameters: $1 - Knative Serving version "HEAD" or "latest-release". Default is "HEAD".
#             $2 - Knative Monitoring YAML file (optional)
function install_knative_serving() {
  local version=${1:-"HEAD"}
  if [[ -z "${INSTALL_CUSTOM_YAMLS}" ]]; then
    install_knative_serving_standard "$version" "$2"
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

function install_istio() {
  # If no gateway was set on command line, assume Istio
  if [[ -z "${ISTIO_VERSION}" ]]; then
    echo ">> No gateway set up on command line, using Istio"
    readonly ISTIO_VERSION="1.4-latest"
  fi

  local istio_base="./third_party/istio-${ISTIO_VERSION}"
  INSTALL_ISTIO_CRD_YAML="${istio_base}/istio-crds.yaml"
  if (( MESH )); then
    INSTALL_ISTIO_YAML="${istio_base}/istio-ci-mesh.yaml"
  else
    INSTALL_ISTIO_YAML="${istio_base}/istio-ci-no-mesh.yaml"
  fi

  echo "Istio CRD YAML: ${INSTALL_ISTIO_CRD_YAML}"
  echo "Istio YAML: ${INSTALL_ISTIO_YAML}"

  echo ">> Bringing up Istio"
  echo ">> Running Istio CRD installer"
  kubectl apply -f "${INSTALL_ISTIO_CRD_YAML}" || return 1
  wait_until_batch_job_complete istio-system || return 1
  UNINSTALL_LIST+=( "${INSTALL_ISTIO_CRD_YAML}" )

  echo ">> Running Istio"
  kubectl apply -f "${INSTALL_ISTIO_YAML}" || return 1
  UNINSTALL_LIST+=( "${INSTALL_ISTIO_YAML}" )

  # If the yaml for the Istio Ingress controller is passed, then install it.
  if [[ -n "$1" ]]; then
    echo ">> Installing Istio Ingress"
    echo "Istio Ingress YAML: ${1}"
    # Create temp copy in which we replace knative-serving by the test's system namespace.
    local YAML_NAME=$(mktemp -p $TMP_DIR --suffix=.$(basename "$1"))
    sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${1} > ${YAML_NAME}
    echo "Istio Ingress YAML: $YAML_NAME"
    ko apply -f "${YAML_NAME}" --selector=networking.knative.dev/ingress-provider=istio || return 1
    UNINSTALL_LIST+=( "${YAML_NAME}" )
  fi
}

function install_gloo() {
  local INSTALL_GLOO_YAML="./third_party/gloo-latest/gloo.yaml"
  echo "Gloo YAML: ${INSTALL_GLOO_YAML}"
  echo ">> Bringing up Gloo"

  kubectl apply -f ${INSTALL_GLOO_YAML} || return 1
  UNINSTALL_LIST+=( "${INSTALL_GLOO_YAML}" )

  echo ">> Patching Gloo"
  # Scale replicas of the Gloo proxies to handle large qps
  kubectl scale -n gloo-system deployment knative-external-proxy --replicas=6
  kubectl scale -n gloo-system deployment knative-internal-proxy --replicas=6
}

function install_kourier() {
  local INSTALL_KOURIER_YAML="./third_party/kourier-latest/kourier.yaml"
  echo "Kourier YAML: ${INSTALL_KOURIER_YAML}"
  echo ">> Bringing up Kourier"

  kubectl apply -f ${INSTALL_KOURIER_YAML} || return 1
  UNINSTALL_LIST+=( "${INSTALL_KOURIER_YAML}" )

  echo ">> Patching Kourier"
  # Scale replicas of the Kourier gateways to handle large qps
  kubectl scale -n kourier-system deployment 3scale-kourier-gateway --replicas=6
}

function install_ambassador() {
  local AMBASSADOR_MANIFESTS_PATH="./third_party/ambassador-latest/"
  echo "Ambassador YAML: ${AMBASSADOR_MANIFESTS_PATH}"

  echo ">> Creating namespace 'ambassador'"
  kubectl create namespace ambassador || return 1

  echo ">> Installing Ambassador"
  kubectl apply -n ambassador -f ${AMBASSADOR_MANIFESTS_PATH} || return 1
  UNINSTALL_LIST+=( "${AMBASSADOR_MANIFESTS_PATH}" )

#  echo ">> Fixing Ambassador's permissions"
#  kubectl patch clusterrolebinding ambassador -p '{"subjects":[{"kind": "ServiceAccount", "name": "ambassador", "namespace": "ambassador"}]}' || return 1

#  echo ">> Enabling Knative support in Ambassador"
#  kubectl set env --namespace ambassador deployments/ambassador AMBASSADOR_KNATIVE_SUPPORT=true || return 1

  echo ">> Patching Ambassador"
  # Scale replicas of the Ambassador gateway to handle large qps
  kubectl scale -n ambassador deployment ambassador --replicas=6
}

function install_contour() {
  local INSTALL_CONTOUR_YAML="./third_party/contour-latest/contour.yaml"
  local INSTALL_NET_CONTOUR_YAML="./third_party/contour-latest/net-contour.yaml"
  echo "Contour YAML: ${INSTALL_CONTOUR_YAML}"
  echo "Contour KIngress YAML: ${INSTALL_NET_CONTOUR_YAML}"

  echo ">> Bringing up Contour"
  kubectl apply -f ${INSTALL_CONTOUR_YAML} || return 1

  UNINSTALL_LIST+=( "${INSTALL_CONTOUR_YAML}" )

  local NET_CONTOUR_YAML_NAME=${TMP_DIR}/${INSTALL_NET_CONTOUR_YAML##*/}
  sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${INSTALL_NET_CONTOUR_YAML} > ${NET_CONTOUR_YAML_NAME}
  echo ">> Bringing up net-contour"
  kubectl apply -f ${NET_CONTOUR_YAML_NAME} || return 1

  UNINSTALL_LIST+=( "${NET_CONTOUR_YAML_NAME}" )
}

# Installs Knative Serving in the current cluster, and waits for it to be ready.
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

  echo ">> Installing Ingress"
  if [[ -n "${GLOO_VERSION}" ]]; then
    install_gloo || return 1
  elif [[ -n "${KOURIER_VERSION}" ]]; then
    install_kourier || return 1
  elif [[ -n "${AMBASSADOR_VERSION}" ]]; then
    install_ambassador || return 1
  elif [[ -n "${CONTOUR_VERSION}" ]]; then
    install_contour || return 1
  else
    if [[ "$1" == "HEAD" ]]; then
      install_istio "./third_party/net-istio.yaml" || return 1
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

  echo ">> Installing Cert-Manager"
  readonly INSTALL_CERT_MANAGER_YAML="./third_party/cert-manager-${CERT_MANAGER_VERSION}/cert-manager.yaml"
  echo "Cert Manager YAML: ${INSTALL_CERT_MANAGER_YAML}"
  kubectl apply -f "${INSTALL_CERT_MANAGER_YAML}" --validate=false || return 1
  UNINSTALL_LIST+=( "${INSTALL_CERT_MANAGER_YAML}" )
  readonly NET_CERTMANAGER_YAML="./third_party/cert-manager-${CERT_MANAGER_VERSION}/net-certmanager.yaml"
  echo "net-certmanager YAML: ${NET_CERTMANAGER_YAML}"
  local CERT_YAML_NAME=${TMP_DIR}/${NET_CERTMANAGER_YAML##*/}
  sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${NET_CERTMANAGER_YAML} > ${CERT_YAML_NAME}
  kubectl apply \
      -f "${CERT_YAML_NAME}" || return 1
  UNINSTALL_LIST+=( "${CERT_YAML_NAME}" )

  echo ">> Installing Knative serving"
  if [[ "$1" == "HEAD" ]]; then
    local CORE_YAML_NAME=${TMP_DIR}/${SERVING_CORE_YAML##*/}
    sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${SERVING_CORE_YAML} > ${CORE_YAML_NAME}
    local HPA_YAML_NAME=${TMP_DIR}/${SERVING_HPA_YAML##*/}
    sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${SERVING_HPA_YAML} > ${HPA_YAML_NAME}
    local POST_INSTALL_JOBS_YAML_NAME=${TMP_DIR}/${SERVING_POST_INSTALL_JOBS_YAML##*/}
    sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${SERVING_POST_INSTALL_JOBS_YAML} > ${POST_INSTALL_JOBS_YAML_NAME}

    echo "Knative YAML: ${CORE_YAML_NAME} and ${HPA_YAML_NAME}"
    kubectl apply \
	    -f "${CORE_YAML_NAME}" \
	    -f "${HPA_YAML_NAME}" || return 1
    UNINSTALL_LIST+=( "${CORE_YAML_NAME}" "${HPA_YAML_NAME}" )
    kubectl create -f ${POST_INSTALL_JOBS_YAML_NAME}

    if (( INSTALL_MONITORING )); then
      echo ">> Installing Monitoring"
      echo "Knative Monitoring YAML: ${MONITORING_YAML}"
      kubectl apply -f "${MONITORING_YAML}" || return 1
      UNINSTALL_LIST+=( "${MONITORING_YAML}" )
    fi
  else
    echo "Knative YAML: ${SERVING_RELEASE_YAML}"
    # We use ko because it has better filtering support for CRDs.
    ko apply -f "${SERVING_RELEASE_YAML}" || return 1
    ko create -f "${SERVING_POST_INSTALL_JOBS_YAML}" || return 1
    UNINSTALL_LIST+=( "${SERVING_RELEASE_YAML}" )

    if (( INSTALL_MONITORING )); then
      echo ">> Installing Monitoring"
      echo "Knative Monitoring YAML: ${2}"
      kubectl apply -f "${2}" || return 1
      UNINSTALL_LIST+=( "${2}" )
    fi
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

# Add function call to trap
# Parameters: $1 - Function to call
#             $2...$n - Signals for trap
function add_trap() {
  local cmd=$1
  shift
  for trap_signal in $@; do
    local current_trap="$(trap -p $trap_signal | cut -d\' -f2)"
    local new_cmd="($cmd)"
    [[ -n "${current_trap}" ]] && new_cmd="${current_trap};${new_cmd}"
    trap -- "${new_cmd}" $trap_signal
  done
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
  kail > ${ARTIFACTS}/k8s.log-$(basename ${E2E_SCRIPT}).txt &
  local kail_pid=$!
  # Clean up kail so it doesn't interfere with job shutting down
  add_trap "kill $kail_pid || true" EXIT

  local TEST_CONFIG_DIR=${TEST_DIR}/config
  echo ">> Creating test resources (${TEST_CONFIG_DIR}/)"
  ko apply ${KO_FLAGS} -f ${TEST_CONFIG_DIR}/ || return 1
  if (( MESH )); then
    kubectl label namespace serving-tests istio-injection=enabled
    kubectl label namespace serving-tests-alt istio-injection=enabled
    kubectl label namespace serving-tests-security istio-injection=enabled
    ko apply ${KO_FLAGS} -f ${TEST_CONFIG_DIR}/security/ --selector=test.knative.dev/dependency=istio-sidecar || return 1
  fi

  echo ">> Uploading test images..."
  ${REPO_ROOT_DIR}/test/upload-test-images.sh || return 1

  echo ">> Waiting for Serving components to be running..."
  wait_until_pods_running ${SYSTEM_NAMESPACE} || return 1

  echo ">> Waiting for Cert Manager components to be running..."
  wait_until_pods_running cert-manager || return 1

  echo ">> Waiting for Ingress provider to be running..."
  if [[ -n "${ISTIO_VERSION}" ]]; then
    wait_until_pods_running istio-system || return 1
    wait_until_service_has_external_http_address istio-system istio-ingressgateway
  fi
  if [[ -n "${GLOO_VERSION}" ]]; then
    # we must set these override values to allow the test spoofing client to work with Gloo
    # see https://github.com/knative/pkg/blob/release-0.7/test/ingress/ingress.go#L37
    export GATEWAY_OVERRIDE=knative-external-proxy
    export GATEWAY_NAMESPACE_OVERRIDE=gloo-system
    wait_until_pods_running gloo-system || return 1
    wait_until_service_has_external_ip gloo-system knative-external-proxy
  fi
  if [[ -n "${KOURIER_VERSION}" ]]; then
    # we must set these override values to allow the test spoofing client to work with Kourier
    # see https://github.com/knative/pkg/blob/release-0.7/test/ingress/ingress.go#L37
    export GATEWAY_OVERRIDE=kourier
    export GATEWAY_NAMESPACE_OVERRIDE=kourier-system
    wait_until_pods_running kourier-system || return 1
    wait_until_service_has_external_http_address kourier-system kourier
  fi
  if [[ -n "${AMBASSADOR_VERSION}" ]]; then
    # we must set these override values to allow the test spoofing client to work with Ambassador
    # see https://github.com/knative/pkg/blob/release-0.7/test/ingress/ingress.go#L37
    export GATEWAY_OVERRIDE=ambassador
    export GATEWAY_NAMESPACE_OVERRIDE=ambassador
    wait_until_pods_running ambassador || return 1
    wait_until_service_has_external_http_address ambassador ambassador
  fi
  if [[ -n "${CONTOUR_VERSION}" ]]; then
    # we must set these override values to allow the test spoofing client to work with Contour
    # see https://github.com/knative/pkg/blob/release-0.7/test/ingress/ingress.go#L37
    export GATEWAY_OVERRIDE=envoy
    export GATEWAY_NAMESPACE_OVERRIDE=contour-external
    wait_until_pods_running contour-external || return 1
    wait_until_pods_running contour-internal || return 1
    wait_until_service_has_external_ip "${GATEWAY_NAMESPACE_OVERRIDE}" "${GATEWAY_OVERRIDE}"
  fi

  if (( INSTALL_MONITORING )); then
    echo ">> Waiting for Monitoring to be running..."
    wait_until_pods_running knative-monitoring || return 1
  fi
}

# Delete test resources
function test_teardown() {
  local TEST_CONFIG_DIR=${TMP_DIR}/test/config
  echo ">> Removing test resources (${TEST_CONFIG_DIR}/)"
  ko delete --ignore-not-found=true --now -f ${TEST_CONFIG_DIR}/
  if (( MESH )); then
    ko delete --ignore-not-found=true --now -f ${TEST_CONFIG_DIR}/security/
  fi
  echo ">> Ensuring test namespaces are clean"
  kubectl delete all --all --ignore-not-found --now --timeout 60s -n serving-tests
  kubectl delete --ignore-not-found --now --timeout 60s namespace serving-tests
  kubectl delete all --all --ignore-not-found --now --timeout 60s -n serving-tests-alt
  kubectl delete --ignore-not-found --now --timeout 60s namespace serving-tests-alt
  kubectl delete all --all --ignore-not-found --now --timeout 60s -n serving-tests-security
  kubectl delete --ignore-not-found --now --timeout 60s namespace serving-tests-security
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

function turn_on_auto_tls() {
  kubectl patch configmap config-network -n ${SYSTEM_NAMESPACE} -p '{"data":{"autoTLS":"Enabled"}}'
}

function turn_off_auto_tls() {
  kubectl patch configmap config-network -n ${SYSTEM_NAMESPACE} -p '{"data":{"autoTLS":"Disabled"}}'
}
