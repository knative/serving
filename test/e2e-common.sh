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
E2E_CLUSTER_MACHINE=${E2E_CLUSTER_MACHINE:-n1-standard-8}

# This script provides helper methods to perform cluster actions.
source $(dirname $0)/../vendor/knative.dev/test-infra/scripts/e2e-tests.sh

CERT_MANAGER_VERSION="0.9.1"
ISTIO_VERSION=""
GLOO_VERSION=""
KOURIER_VERSION=""

HTTPS=0

# Current YAMLs used to install Knative Serving.
INSTALL_RELEASE_YAML=""
INSTALL_MONITORING_YAML=""

INSTALL_MONITORING=0

GATEWAY_SETUP=0
RECONCILE_GATEWAY=0

# List of custom YAMLs to install, if specified (space-separated).
INSTALL_CUSTOM_YAMLS=""

# Parse our custom flags.
function parse_flags() {
  case "$1" in
    --istio-version)
      [[ $2 =~ ^[0-9]+\.[0-9]+(\.[0-9]+|\-latest)$ ]] || abort "version format must be '[0-9].[0-9].[0-9]' or '[0-9].[0-9]-latest"
      readonly ISTIO_VERSION=$2
      GATEWAY_SETUP=1
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
    --reconcile-gateway)
      readonly RECONCILE_GATEWAY=1
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
      GATEWAY_SETUP=1
      return 2
      ;;
    --kourier-version)
      # currently, the value of --kourier-version is ignored
      # latest version of Kourier pinned in third_party will be installed
      readonly KOURIER_VERSION=$2
      GATEWAY_SETUP=1
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

  # Set ko flags to omit istio resources from generated YAMLs
  if [[ -z "${ISTIO_VERSION}" ]]; then
    KO_FLAGS="${KO_FLAGS} --selector=networking.knative.dev/ingress-provider!=istio"
  fi

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
# Parameters: $1 - Knative Serving YAML file
#             $2 - Knative Monitoring YAML file (optional)
function install_knative_serving() {
  if [[ -z "${INSTALL_CUSTOM_YAMLS}" ]]; then
    install_knative_serving_standard "$1" "$2"
    return
  fi
  echo ">> Installing Knative serving from custom YAMLs"
  echo "Custom YAML files: ${INSTALL_CUSTOM_YAMLS}"
  for yaml in ${INSTALL_CUSTOM_YAMLS}; do
    echo "Installing '${yaml}'"
    kubectl create -f "${yaml}" || return 1
  done
}

function install_istio() {
  local istio_base="./third_party/istio-${ISTIO_VERSION}"
  INSTALL_ISTIO_CRD_YAML="${istio_base}/istio-crds.yaml"
  (( MESH )) && INSTALL_ISTIO_YAML="${istio_base}/istio.yaml" || INSTALL_ISTIO_YAML="${istio_base}/istio-lean.yaml"

  echo "Istio CRD YAML: ${INSTALL_ISTIO_CRD_YAML}"
  echo "Istio YAML: ${INSTALL_ISTIO_YAML}"

  echo ">> Bringing up Istio"
  echo ">> Running Istio CRD installer"
  kubectl apply -f "${INSTALL_ISTIO_CRD_YAML}" || return 1
  wait_until_batch_job_complete istio-system || return 1

  echo ">> Bringing up Istio"
  echo ">> Running Istio CRD installer"
  kubectl apply -f "${INSTALL_ISTIO_CRD_YAML}" || return 1
  wait_until_batch_job_complete istio-system || return 1

  echo ">> Running Istio"
  kubectl apply -f "${INSTALL_ISTIO_YAML}" || return 1
}

function install_gloo() {
  local gloo_base="./third_party/gloo-latest"
  INSTALL_GLOO_YAML="${gloo_base}/gloo.yaml"
  echo "Gloo YAML: ${INSTALL_GLOO_YAML}"
  echo ">> Bringing up Gloo"

  kubectl apply -f ${INSTALL_GLOO_YAML} || return 1
}

function install_kourier() {
  local kourier_base="./third_party/kourier-latest"
  INSTALL_KOURIER_YAML="${kourier_base}/kourier.yaml"
  echo "Kourier YAML: ${INSTALL_KOURIER_YAML}"
  echo ">> Bringing up Kourier"

  kubectl apply -f ${INSTALL_KOURIER_YAML} || return 1
}

# Installs Knative Serving in the current cluster, and waits for it to be ready.
# If no parameters are passed, installs the current source-based build.
# Parameters: $1 - Knative Serving YAML file
#             $2 - Knative Monitoring YAML file (optional)
function install_knative_serving_standard() {
  INSTALL_RELEASE_YAML=$1
  INSTALL_MONITORING_YAML=$2
  if [[ -z "$1" ]]; then
    # install_knative_serving_standard was called with no arg.
    build_knative_from_source
    INSTALL_RELEASE_YAML="${SERVING_YAML}"

    # install serving core if installing for Gloo or Kourier
    if [[ -n "${GLOO_VERSION}" || -n "${KOURIER_VERSION}" ]]; then
      INSTALL_RELEASE_YAML="${SERVING_CORE_YAML}"
    fi

    if (( INSTALL_MONITORING )); then
      INSTALL_MONITORING_YAML="${MONITORING_YAML}"
    fi
  fi

  INSTALL_CERT_MANAGER_YAML="./third_party/cert-manager-${CERT_MANAGER_VERSION}/cert-manager.yaml"

  echo ">> Installing Knative serving"
  echo "Cert Manager YAML: ${INSTALL_CERT_MANAGER_YAML}"
  echo "Knative YAML: ${INSTALL_RELEASE_YAML}"

  # If no gateway was set on command line, assume Istio
  if (( ! GATEWAY_SETUP )); then
    echo ">> No gateway set up on command line, using Istio"
    readonly ISTIO_VERSION="1.4-latest"
  fi

  if [[ -n "${ISTIO_VERSION}" ]]; then
    install_istio
  fi
  if [[ -n "${GLOO_VERSION}" ]]; then
    install_gloo
  fi
  if [[ -n "${KOURIER_VERSION}" ]]; then
    install_kourier
  fi

  echo ">> Installing Cert-Manager"
  kubectl apply -f "${INSTALL_CERT_MANAGER_YAML}" --validate=false || return 1

  echo ">> Bringing up Serving"
  kubectl apply -f "${INSTALL_RELEASE_YAML}" || return 1

  if [[ -n "${KOURIER_VERSION}" ]]; then
    echo ">> Making Kourier the default ingress"
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-network
  namespace: knative-serving
  labels:
    serving.knative.dev/release: devel
data:
  ingress.class: "kourier.ingress.networking.knative.dev"
  clusteringress.class: "kourier.ingress.networking.knative.dev"
EOF
  fi

  if (( RECONCILE_GATEWAY )); then
    echo ">> Turning on reconcileExternalGateway"
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-istio
  namespace: knative-serving
  labels:
    serving.knative.dev/release: devel
    networking.knative.dev/ingress-provider: istio
data:
  reconcileExternalGateway: "true"
EOF
  fi

  echo ">> Turning on profiling.enable"
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-observability
  namespace: knative-serving
data:
  profiling.enable: "true"
EOF

  echo ">> Patching activator hpa"
  # We set min replicas to 2 for testing multiple activator pods.
  kubectl -n knative-serving patch hpa activator --patch '{"spec":{"minReplicas":2}}' || return 1

  # post-install steps for istio
  if [[ -n "${ISTIO_VERSION}" ]]; then
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
      kubectl patch hpa -n istio-system istio-pilot \
        --patch '{"spec": {"minReplicas": 3, "maxReplicas": 10, "targetCPUUtilizationPercentage": 60}}' || return 1
    else
      # Some versions of Istio don't provide an HPA for pilot.
      kubectl autoscale -n istio-system deploy istio-pilot --min=3 --max=10 --cpu-percent=60 || return 1
    fi
  elif [[ -n "${GLOO_VERSION}" ]]; then
    # Scale replicas of the Gloo proxies to handle large qps
    kubectl scale -n gloo-system deployment knative-external-proxy --replicas=6
    kubectl scale -n gloo-system deployment knative-internal-proxy --replicas=6
  elif [[ -n "${KOURIER_VERSION}" ]]; then
    # Scale replicas of the Kourier gateways to handle large qps
    kubectl scale -n kourier-system deployment 3scale-kourier-gateway --replicas=6
  fi

  if [[ -n "${INSTALL_MONITORING_YAML}" ]]; then
    echo ">> Installing Monitoring"
    echo "Knative Monitoring YAML: ${INSTALL_MONITORING_YAML}"
    kubectl apply -f "${INSTALL_MONITORING_YAML}" || return 1
  fi
}

# Check if we should use --resolvabledomain.  In case the ingress only has
# hostname, we doesn't yet have a way to support resolvable domain in tests.
function use_resolvable_domain() {
  # Temporarily turning off xip.io tests, as DNS errors aren't always retried.
  echo "false"
}

# Check if we should use --https.
function use_https() {
  if (( HTTPS )); then
    echo "--https"
  else
    echo ""
  fi
}

# Uninstalls Knative Serving from the current cluster.
function knative_teardown() {
  if [[ -z "${INSTALL_CUSTOM_YAMLS}" && -z "${INSTALL_RELEASE_YAML}" ]]; then
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
    echo "Istio YAML: ${INSTALL_ISTIO_YAML}"
    echo "Cert-Manager YAML: ${INSTALL_CERT_MANAGER_YAML}"
    echo "Knative YAML: ${INSTALL_RELEASE_YAML}"
    echo ">> Bringing down Serving"
    ko delete --ignore-not-found=true -f "${INSTALL_RELEASE_YAML}" || return 1
    if [[ -n "${INSTALL_MONITORING_YAML}" ]]; then
      echo ">> Bringing down monitoring"
      ko delete --ignore-not-found=true -f "${INSTALL_MONITORING_YAML}" || return 1
    fi
    echo ">> Bringing down Istio"
    kubectl delete --ignore-not-found=true -f "${INSTALL_ISTIO_YAML}" || return 1
    echo ">> Bringing down Cert-Manager"
    kubectl delete --ignore-not-found=true -f "${INSTALL_CERT_MANAGER_YAML}" || return 1
  fi
}

# Create test resources and images
function test_setup() {
  echo ">> Setting up logging..."

  # Install kail if needed.
  if ! which kail > /dev/null; then
    bash <( curl -sfL https://raw.githubusercontent.com/boz/kail/master/godownloader.sh) -b "$GOPATH/bin"
  fi

  # Capture all logs.
  kail > ${ARTIFACTS}/k8s.log.txt &
  local kail_pid=$!
  # Clean up kail so it doesn't interfere with job shutting down
  trap "kill $kail_pid || true" EXIT

  echo ">> Creating test resources (test/config/)"
  ko apply ${KO_FLAGS} -f test/config/ || return 1
  ${REPO_ROOT_DIR}/test/upload-test-images.sh || return 1
  wait_until_pods_running knative-serving || return 1
  if [[ -n "${ISTIO_VERSION}" ]]; then
    wait_until_pods_running istio-system || return 1
    wait_until_service_has_external_ip istio-system istio-ingressgateway
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
    export GATEWAY_OVERRIDE=kourier-external
    export GATEWAY_NAMESPACE_OVERRIDE=kourier-system
    wait_until_pods_running kourier-system || return 1
    wait_until_service_has_external_ip kourier-system kourier-external
  fi
  if [[ -n "${INSTALL_MONITORING_YAML}" ]]; then
    wait_until_pods_running knative-monitoring || return 1
  fi
}

# Delete test resources
function test_teardown() {
  echo ">> Removing test resources (test/config/)"
  ko delete --ignore-not-found=true --now -f test/config/
  (( MESH )) && ko delete --ignore-not-found=true --now -f test/config/mtls/
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
