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

# Default Istio configuration to install: 1.2-latest, no mesh, cert manager 0.6.1.
ISTIO_VERSION="1.2-latest"
ISTIO_MESH=0
CERT_MANAGER_VERSION="0.6.1"

# Current YAMLs used to install Knative Serving.
INSTALL_RELEASE_YAML=""
INSTALL_MONITORING_YAML=""

INSTALL_MONITORING=0

INSTALL_BETA=1

RECONCILE_GATEWAY=0

# List of custom YAMLs to install, if specified (space-separated).
INSTALL_CUSTOM_YAMLS=""

# Parse our custom flags.
function parse_flags() {
  case "$1" in
    --istio-version)
      [[ $2 =~ ^[0-9]+\.[0-9]+(\.[0-9]+|\-latest)$ ]] || abort "version format must be '[0-9].[0-9].[0-9]' or '[0-9].[0-9]-latest"
      readonly ISTIO_VERSION=$2
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
      readonly ISTIO_MESH=1
      return 1
      ;;
    --no-mesh)
      readonly ISTIO_MESH=0
      return 1
      ;;
    --install-monitoring)
      readonly INSTALL_MONITORING=1
      return 1
      ;;
    --install-alpha)
      readonly INSTALL_BETA=0
      return 1
      ;;
    --install-beta)
      readonly INSTALL_BETA=1
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
      [[ $2 =~ ^[0-9]+\.[0-9]+(\.[0-9]+|\-latest)$ ]] || abort "version format must be '[0-9].[0-9].[0-9]' or '[0-9].[0-9]-latest"
      readonly GLOO_VERSION=$2
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

  # set ko flags to omit istio resources from generated YAMLs
  if [[ -n "${GLOO_VERSION}" ]]; then
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
  kubectl label namespace default istio-injection=enabled
  echo "Custom YAML files: ${INSTALL_CUSTOM_YAMLS}"
  for yaml in ${INSTALL_CUSTOM_YAMLS}; do
    echo "Installing '${yaml}'"
    kubectl create -f "${yaml}" || return 1
  done
}

function install_istio() {
  local istio_base="./third_party/istio-${ISTIO_VERSION}"
  INSTALL_ISTIO_CRD_YAML="${istio_base}/istio-crds.yaml"
  (( ISTIO_MESH )) && INSTALL_ISTIO_YAML="${istio_base}/istio.yaml" || INSTALL_ISTIO_YAML="${istio_base}/istio-lean.yaml"

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
  local gloo_base="./third_party/gloo-${GLOO_VERSION}"
  INSTALL_GLOO_YAML="${gloo_base}/gloo.yaml"
  echo "Gloo YAML: ${INSTALL_GLOO_YAML}"
  echo ">> Bringing up Gloo"

  kubectl apply -f ${INSTALL_GLOO_YAML} || return 1
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
    if (( INSTALL_BETA )); then
      INSTALL_RELEASE_YAML="${SERVING_BETA_YAML}"
    else
      INSTALL_RELEASE_YAML="${SERVING_ALPHA_YAML}"
    fi
    if (( INSTALL_MONITORING )); then
      INSTALL_MONITORING_YAML="${MONITORING_YAML}"
    fi
  fi

  INSTALL_CERT_MANAGER_YAML="./third_party/cert-manager-${CERT_MANAGER_VERSION}/cert-manager.yaml"

  echo ">> Installing Knative serving"
  echo "Cert Manager YAML: ${INSTALL_CERT_MANAGER_YAML}"
  echo "Knative YAML: ${INSTALL_RELEASE_YAML}"

  if [[ -z "${GLOO_VERSION}" ]]; then
    # install istio as the default knative ingress
    install_istio
  else
    # install gloo if $GLOO_VERSION is provided
    install_gloo
  fi

  echo ">> Installing Cert-Manager"
  kubectl apply -f "${INSTALL_CERT_MANAGER_YAML}" --validate=false || return 1

  echo ">> Bringing up Serving"
  kubectl apply -f "${INSTALL_RELEASE_YAML}" || return 1

  if (( RECONCILE_GATEWAY )); then
    echo ">>Turning on reconcileExternalGateway."
    kubectl get cm config-istio -n knative-serving -o yaml | \
      sed 's/  reconcileExternalGateway: \"false\"/reconcileExternalGateway: \"true\"/g' |\
        kubectl replace -f -
  fi

  echo ">> Adding more activator pods."
  # This command would fail if the HPA already exist, like during upgrade test.
  # Therefore we don't exit on failure, and don't log an error message.
  kubectl autoscale deploy --min=2 --max=2 -n knative-serving activator 2>/dev/null

  # post-install steps for istio
  if [[ -z "${GLOO_VERSION}" ]]; then
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

  # Install kail.
  bash <( curl -sfL https://raw.githubusercontent.com/boz/kail/master/godownloader.sh) -b "$GOPATH/bin"

  # Capture all logs.
  kail > ${ARTIFACTS}/k8s.log.txt &

  echo ">> Creating test resources (test/config/)"
  ko apply ${KO_FLAGS} -f test/config/ || return 1
  ${REPO_ROOT_DIR}/test/upload-test-images.sh || return 1
  wait_until_pods_running knative-serving || return 1
  if [[ -z "${GLOO_VERSION}" ]]; then
    wait_until_pods_running istio-system || return 1
    wait_until_service_has_external_ip istio-system istio-ingressgateway
  else
    # we must set these override values to allow the test spoofing client to work with Gloo
    # see https://github.com/knative/pkg/blob/release-0.7/test/ingress/ingress.go#L37
    export GATEWAY_OVERRIDE=clusteringress-proxy
    export GATEWAY_NAMESPACE_OVERRIDE=gloo-system
    wait_until_pods_running gloo-system || return 1
    wait_until_service_has_external_ip gloo-system clusteringress-proxy
  fi
  if [[ -n "${INSTALL_MONITORING_YAML}" ]]; then
    wait_until_pods_running knative-monitoring || return 1
  fi
}

# Delete test resources
function test_teardown() {
  echo ">> Removing test resources (test/config/)"
  ko delete --ignore-not-found=true --now -f test/config/
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
