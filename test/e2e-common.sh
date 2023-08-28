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

export CERT_MANAGER_VERSION=${CERT_MANAGER_VERSION:-"latest"}
# Since default is istio, make default ingress as istio
export INGRESS_CLASS=${INGRESS_CLASS:-istio.ingress.networking.knative.dev}
export ISTIO_VERSION=${ISTIO_VERSION:-"latest"}
export KOURIER_VERSION=${KOURIER_VERSION:-""}
export CONTOUR_VERSION=${CONTOUR_VERSION:-""}
export GATEWAY_API_VERSION=${GATEWAY_API_VERSION:-""}
export CERTIFICATE_CLASS=${CERTIFICATE_CLASS:-""}
# Only build linux/amd64 bit images
export KO_FLAGS="${KO_FLAGS:---platform=linux/amd64}"

export RUN_HTTP01_AUTO_TLS_TESTS=${RUN_HTTP01_AUTO_TLS_TESTS:-0}
export HTTPS=${HTTPS:-0}
export SHORT=${SHORT:-0}
export ENABLE_HA=${ENABLE_HA:-0}
export ENABLE_TLS=${ENABLE_TLS:-0}
export MESH=${MESH:-0}
export AMBIENT=${AMBIENT:-0}
export PERF=${PERF:-0}
export KIND=${KIND:-0}
export CLUSTER_DOMAIN=${CLUSTER_DOMAIN:-cluster.local}

# List of custom YAMLs to install, if specified (space-separated).
export INSTALL_CUSTOM_YAMLS=${INSTALL_CUSTOM_YAMLS:-""}
export INSTALL_SERVING_VERSION=${INSTALL_SERVING_VERSION:-"HEAD"}
export INSTALL_ISTIO_VERSION=${INSTALL_ISTIO_VERSION:-"HEAD"}
export YTT_FILES=()

export TMP_DIR="${TMP_DIR:-$(mktemp -d -t ci-$(date +%Y-%m-%d-%H-%M-%S)-XXXXXXXXXX)}"

readonly E2E_YAML_DIR=${E2E_YAML_DIR:-"${TMP_DIR}/e2e-yaml"}

# This the namespace used to install Knative Serving. Use generated UUID as namespace.
export SYSTEM_NAMESPACE="${SYSTEM_NAMESPACE:-$(uuidgen | tr 'A-Z' 'a-z')}"

# Keep this in sync with test/ha/ha.go
readonly REPLICAS=${REPLICAS:-3}
readonly BUCKETS=${BUCKETS:-10}

export PVC=${PVC:-1}
export QUOTA=${QUOTA:-1}

# Receives the latest serving version and searches for the same version with major and minor and searches for the latest patch
function latest_net_istio_version() {
  local serving_version=$1
  local major_minor=$(echo "$serving_version" | cut -d '.' -f 1,2)

  local url="https://api.github.com/repos/knative/net-istio/releases"
  local curl_output=$(mktemp)
  local curl_flags='-L --show-error --silent'

  if [ -n "${GITHUB_TOKEN-}" ]; then
    curl $curl_flags -H "Authorization: Bearer $GITHUB_TOKEN" $url > $curl_output
  else
    curl $curl_flags $url > $curl_output
  fi

  jq --arg major_minor "$major_minor" -r \
    '[.[].tag_name] |
    map(select(. | startswith($major_minor))) |
    sort_by( sub("knative-";"") |
    sub("v";"") |
    split(".") |
    map(tonumber) ) |
    reverse[0]' $curl_output
}

# Latest serving release. If user does not supply this as a flag, the latest
# tagged release on the current branch will be used.
LATEST_SERVING_RELEASE_VERSION=$(latest_version)

# Latest net-istio release.
LATEST_NET_ISTIO_RELEASE_VERSION=$(latest_net_istio_version "$LATEST_SERVING_RELEASE_VERSION")


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
    --install-latest-release)
      INSTALL_SERVING_VERSION="latest-release"
      INSTALL_ISTIO_VERSION="latest-release"
      return 1
      ;;
    --cert-manager-version)
      [[ $2 =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || abort "version format must be '[0-9].[0-9].[0-9]'"
      readonly CERT_MANAGER_VERSION=$2
      readonly CERTIFICATE_CLASS="cert-manager.certificate.networking.knative.dev"
      return 2
      ;;
    --run-http01-auto-tls-tests)
      readonly RUN_HTTP01_AUTO_TLS_TESTS=1
      return 1
      ;;
    --mesh)
      readonly MESH=1
      return 1
      ;;
    --no-mesh)
      readonly MESH=0
      return 1
      ;;
    --perf)
      readonly PERF=1
      return 1
      ;;
    --enable-ha)
      readonly ENABLE_HA=1
      return 1
      ;;
    --kind)
      readonly KIND=1
      return 1
      ;;
    --https)
      readonly HTTPS=1
      return 1
      ;;
    --short)
      readonly SHORT=1
      return 1
      ;;
    --cluster-domain)
      [[ -z "$2" ]] && fail_test "Missing argument to --cluster-domain"
      readonly CLUSTER_DOMAIN="$2"
      return 2
      ;;
    --custom-yamls)
      [[ -z "$2" ]] && fail_test "Missing argument to --custom-yamls"
      INSTALL_CUSTOM_YAMLS="${2}"
      readonly INSTALL_CUSTOM_YAMLS
      return 2
      ;;
    --kourier-version)
      # currently, the value of --kourier-version is ignored
      # latest version of Kourier pinned in third_party will be installed
      readonly KOURIER_VERSION=$2
      readonly INGRESS_CLASS="kourier.ingress.networking.knative.dev"
      return 2
      ;;
    --contour-version)
      # currently, the value of --contour-version is ignored
      # latest version of Contour pinned in third_party will be installed
      readonly CONTOUR_VERSION=$2
      readonly INGRESS_CLASS="contour.ingress.networking.knative.dev"
      return 2
      ;;
    --gateway-api-version)
      # currently, the value of --gateway-api-version is ignored
      # latest version of Contour pinned in third_party will be installed
      readonly GATEWAY_API_VERSION=$2
      readonly INGRESS_CLASS="gateway-api.ingress.networking.knative.dev"
      readonly SHORT=1
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

# Gather all the YAML we require to run all our tests.
# We stage these files into ${E2E_YAML_DIR}
#
# > serving built from HEAD       > $E2E_YAML_DIR/serving/HEAD/install
#                                 > $E2E_YAML_DIR/serving/HEAD/post-install
#
# > serving latest-release        > $E2E_YAML_DIR/serving/latest-release/install
#                                 > $E2E_YAML_DIR/serving/latest-release/post-install
#
# > net-istio HEAD                > $E2E_YAML_DIR/istio/HEAD/install
# > net-istio latest-release      > $E2E_YAML_DIR/istio/latest-release/install
#
#   We download istio.yaml for our given test profile (ie. mesh on kind).
#   The files downloaded are istio.yaml & config-istio.yaml.
#
#   config-istio.yaml is to be applied _after_ we install net-istio.yaml since
#   it includes profile specific configuration
#
# > test/config/**.yaml           > $E2E_YAML_DIR/serving/test/config
#
#   These resources will be passed through `ko` if there exists a `ko://`
#   strict reference. Secondly namespace overlays will be applied to place
#   them in the correct
#
function knative_setup() {
  local need_latest_version=0

  if [[ "${INSTALL_SERVING_VERSION}" == "latest-release" ]]; then
    need_latest_version=1
  fi

  if [[ -z "${INSTALL_CUSTOM_YAMLS}" ]]; then
    stage_serving_head
  else
    stage_serving_custom
  fi

  # Download resources we need for upgrade tests
  if (( need_latest_version )); then
    stage_serving_latest
  fi

  # Download Istio YAMLs
  if is_ingress_class istio; then
    stage_istio_head

    # Download istio resources we need for upgrade tests
    if (( need_latest_version )); then
      stage_istio_latest
    fi
  fi

  # Install gateway-api and istio. Gateway API CRD must be installed before Istio.
  if is_ingress_class gateway-api; then
    stage_gateway_api_resources
  fi

  stage_test_resources

  install "${INSTALL_SERVING_VERSION}" "${INSTALL_ISTIO_VERSION}"
}

# Installs Knative Serving in the current cluster.
# If no parameters are passed, installs the current source-based build, unless custom
# YAML files were passed using the --custom-yamls flag.
# Parameters: $1 - serving version "HEAD" or "latest-release". Default is "HEAD".
# Parameters: $2 - ingress version "HEAD" or "latest-release". Default is "HEAD".
#
# TODO - ingress version toggle only works for istio
# TODO - allow latest-release for cert-manager
function install() {
  header "Installing Knative Serving"

  local ingress=${INGRESS_CLASS%%.*}
  local serving_version="${1:-"HEAD"}"
  local ingress_version="${2:-"HEAD"}"

  YTT_FILES=(
    "${REPO_ROOT_DIR}/test/config/ytt/lib"
    "${REPO_ROOT_DIR}/test/config/ytt/values.yaml"

    # see cluster_setup for how the files are staged
    "${E2E_YAML_DIR}/test/config/cluster-resources.yaml"
    "${E2E_YAML_DIR}/test/config/test-resources.yaml"
    "${E2E_YAML_DIR}/serving/${serving_version}/install"

    "${REPO_ROOT_DIR}/test/config/ytt/rename-namespaces.yaml"
    "${REPO_ROOT_DIR}/test/config/ytt/core"
  )

  if is_ingress_class istio; then
    # Istio - see cluster_setup for how the files are staged
    YTT_FILES+=("${E2E_YAML_DIR}/istio/${ingress_version}/install")
  elif is_ingress_class gateway-api; then
    # This installs an istio version that works with the v1alpha1 gateway api
    YTT_FILES+=("${E2E_YAML_DIR}/gateway-api/install")
    YTT_FILES+=("${REPO_ROOT_DIR}/third_party/${ingress}-latest")
  else
    YTT_FILES+=("${REPO_ROOT_DIR}/third_party/${ingress}-latest")
  fi

  YTT_FILES+=("${REPO_ROOT_DIR}/test/config/ytt/ingress/${ingress}")
  YTT_FILES+=("${REPO_ROOT_DIR}/test/config/ytt/certmanager/kapp-order.yaml")
  YTT_FILES+=("${REPO_ROOT_DIR}/third_party/cert-manager-${CERT_MANAGER_VERSION}/cert-manager.yaml")
  YTT_FILES+=("${REPO_ROOT_DIR}/third_party/cert-manager-${CERT_MANAGER_VERSION}/net-certmanager.yaml")

  if (( MESH )); then
    YTT_FILES+=("${REPO_ROOT_DIR}/test/config/ytt/mesh")
  fi

  if ((AMBIENT)); then
    YTT_FILES+=("${REPO_ROOT_DIR}/test/config/ytt/ambient")
  fi

  if (( ENABLE_HA )); then
    YTT_FILES+=("${E2E_YAML_DIR}/test/config/chaosduck/chaosduck.yaml")
    YTT_FILES+=("${REPO_ROOT_DIR}/test/config/ytt/ha")
  fi

  if (( PERF )); then
    YTT_FILES+=("${REPO_ROOT_DIR}/test/config/ytt/performance")
  fi

  if (( KIND )); then
    YTT_FILES+=("${REPO_ROOT_DIR}/test/config/ytt/kind/core")
  fi

  if (( PVC )); then
    YTT_FILES+=("${REPO_ROOT_DIR}/test/config/pvc/pvc.yaml")
  fi

  if (( QUOTA )); then
    YTT_FILES+=("${REPO_ROOT_DIR}/test/config/resource-quota/resource-quota.yaml")
  fi

  if (( ENABLE_TLS )); then
    YTT_FILES+=("${REPO_ROOT_DIR}/test/config/tls/cert-secret.yaml")
  fi

  local ytt_result=$(mktemp)
  local ytt_post_install_result=$(mktemp)
  local ytt_flags=""

  for file in "${YTT_FILES[@]}"; do
    if [[ -f "${file}" ]] || [[ -d "${file}" ]]; then
      echo "including ${file}"
      ytt_flags+=" -f ${file}"
    fi
  done

  # use ytt to wrangle the yaml & kapp to apply the resources
  # to the cluster and wait
  run_ytt ${ytt_flags} \
    --data-value serving.namespaces.system="${SYSTEM_NAMESPACE}" \
    --data-value k8s.cluster.domain="${CLUSTER_DOMAIN}" \
    > "${ytt_result}" \
    || fail_test "failed to create deployment configuration"


  # Post install jobs configuration
  run_ytt \
    -f "${REPO_ROOT_DIR}/test/config/ytt/lib" \
    -f "${REPO_ROOT_DIR}/test/config/ytt/values.yaml" \
    -f "${REPO_ROOT_DIR}/test/config/ytt/rename-namespaces.yaml" \
    -f "${REPO_ROOT_DIR}/test/config/ytt/post-install" \
    -f "${E2E_YAML_DIR}/serving/${serving_version}/post-install" \
    --data-value serving.namespaces.system="${SYSTEM_NAMESPACE}" \
    --data-value k8s.cluster.domain="${CLUSTER_DOMAIN}" \
    > "${ytt_post_install_result}" \
    || fail_test "failed to create post-install jobs configuration"


  echo "serving config at ${ytt_result}"
  echo "serving post-install config at ${ytt_post_install_result}"

  run_kapp deploy --yes --app serving --file "${ytt_result}" \
        || fail_test "failed to setup knative"

  run_kapp deploy --yes --app serving-post-install --file "${ytt_post_install_result}" \
        || fail_test "failed to run serving post-install"

  setup_ingress_env_vars

  if (( ENABLE_HA )); then
    # # Changing the bucket count and cycling the controllers will leave around stale
    # # lease resources at the old sharding factor, so clean these up.
    # kubectl -n ${SYSTEM_NAMESPACE} delete leases --all
    wait_for_leader_controller || return 1
  fi

  if (( ENABLE_TLS )); then
    echo "Patch to config-network to enable internal encryption"
    toggle_feature internal-encryption true config-network
    if [[ "$INGRESS_CLASS" == "kourier.ingress.networking.knative.dev" ]]; then
      echo "Point Kourier local gateway to custom server certificates"
      toggle_feature cluster-cert-secret server-certs config-kourier
      # This needs to match the name of Secret in test/config/tls/cert-secret.yaml
      export CA_CERT=ca-cert
      # This needs to match $san from test/config/tls/generate.sh
      export SERVER_NAME=knative.dev
    fi
    echo "Restart activator to mount the certificates"
    kubectl delete pod -n ${SYSTEM_NAMESPACE} -l app=activator
    kubectl wait --timeout=60s --for=condition=Available deployment  -n ${SYSTEM_NAMESPACE} activator
  fi
}

# Check if we should use --resolvabledomain.  In case the ingress only has
# hostname, we doesn't yet have a way to support resolvable domain in tests.
function use_resolvable_domain() {
  # Temporarily turning off sslip.io tests, as DNS errors aren't always retried.
  echo "false"
}

# Uninstalls Knative Serving from the current cluster.
function knative_teardown() {
  run_kapp delete --yes --app "serving-post-install"
  run_kapp delete --yes --app "serving"
}

# Create test resources and images
function test_setup() {
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

  echo ">> Uploading test images..."
  ${REPO_ROOT_DIR}/test/upload-test-images.sh || return 1
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

function install_latest_release() {
  header "Installing Knative latest public release"

  install latest-release latest-release \
      || fail_test "Knative latest release installation failed"
}

function install_head_reuse_ingress() {
  header "Installing Knative head release and reusing ingress"
  # Keep the existing ingress and do not upgrade it. The ingress upgrade
  # makes ongoing requests fail.
  install HEAD latest-release \
    || fail_test "Knative head release installation failed"
}

# Create all manifests required to install Knative Serving.
# This will build everything from the current source.
# All generated YAMLs will be available and pointed by the corresponding
# environment variables as set in /hack/generate-yamls.sh.
function build_knative_from_source() {
  YAML_ENV_FILES="$(mktemp)"
  "${REPO_ROOT_DIR}/hack/generate-yamls.sh" "${REPO_ROOT_DIR}" "$(mktemp)" "${YAML_ENV_FILES}" || fail_test "failed to build"
  source "${YAML_ENV_FILES}"
}

function stage_serving_head() {
  header "Building Serving HEAD"
  build_knative_from_source

  local head_dir="${E2E_YAML_DIR}/serving/HEAD/install"
  local head_post_install_dir="${E2E_YAML_DIR}/serving/HEAD/post-install"

  mkdir -p "${head_dir}"
  mkdir -p "${head_post_install_dir}"

  cp "${SERVING_CORE_YAML}" "${head_dir}"
  cp "${SERVING_HPA_YAML}" "${head_dir}"
  cp "${SERVING_POST_INSTALL_JOBS_YAML}" "${head_post_install_dir}"
}

function stage_serving_custom() {
  source "${INSTALL_CUSTOM_YAMLS}"

  local head_dir="${E2E_YAML_DIR}/serving/HEAD/install"
  local head_post_install_dir="${E2E_YAML_DIR}/serving/HEAD/post-install"

  mkdir -p "${head_dir}"
  mkdir -p "${head_post_install_dir}"

  cp "${SERVING_CORE_YAML}" "${head_dir}"
  cp "${SERVING_HPA_YAML}" "${head_dir}"
  cp "${SERVING_POST_INSTALL_JOBS_YAML}" "${head_post_install_dir}"
}


function stage_serving_latest() {
  header "Staging Serving ${LATEST_SERVING_RELEASE_VERSION}"
  local latest_dir="${E2E_YAML_DIR}/serving/latest-release/install"
  local latest_post_install_dir="${E2E_YAML_DIR}/serving/latest-release/post-install"
  local version="${LATEST_SERVING_RELEASE_VERSION}"

  mkdir -p "${latest_dir}"
  mkdir -p "${latest_post_install_dir}"

  # Download the latest release of Knative Serving.
  local url="https://github.com/knative/serving/releases/download/${version}"

  wget "${url}/serving-core.yaml" -P "${latest_dir}" \
    || fail_test "Unable to download latest knative/serving core file."

  wget "${url}/serving-hpa.yaml" -P "${latest_dir}" \
    || fail_test "Unable to download latest knative/serving hpa file."

  wget "${url}/serving-post-install-jobs.yaml" -P "${latest_post_install_dir}" \
    || fail_test "Unable to download latest knative/serving post install file."
}

function stage_test_resources() {
  header "Staging Test Resources"

  local source_dir="${REPO_ROOT_DIR}/test/config"
  local target_dir="${E2E_YAML_DIR}/test/config"

  mkdir -p "${target_dir}"

  for file in $(find -L "${source_dir}" -type f -name "*.yaml"); do
    if [[ "${file}" == *"test/config/ytt"* ]]; then
      continue
    fi
    target="${file/${source_dir}/$target_dir}"
    mkdir -p $(dirname $target)

    if grep -Fq "ko://" "${file}"; then
      local ko_target="$(mktemp -d)/$(basename $file)"
      echo building "${file/$REPO_ROOT_DIR/}"
      ko resolve $(ko_flags) -f "${file}" > "${ko_target}" || fail_test "failed to build test resource"
      file="${ko_target}"
    fi

    echo templating "${file/$REPO_ROOT_DIR/}" to "${target}"
    overlay_system_namespace "${file}" > "${target}" || fail_test "failed to template"
  done
}

function ko_flags() {
  local KO_YAML_FLAGS="-P"
  local KO_FLAGS="${KO_FLAGS:-}"

  [[ "${KO_DOCKER_REPO}" != gcr.io/* ]] && KO_YAML_FLAGS=""

  if [[ "${KO_FLAGS}" != *"--platform"* ]]; then
    KO_YAML_FLAGS="${KO_YAML_FLAGS} --platform=all"
  fi

  echo "${KO_YAML_FLAGS} ${KO_FLAGS}"
}

function overlay_system_namespace() {
  run_ytt \
      -f "${REPO_ROOT_DIR}/test/config/ytt/lib" \
      -f "${REPO_ROOT_DIR}/test/config/ytt/values.yaml" \
      -f "${REPO_ROOT_DIR}/test/config/ytt/rename-namespaces.yaml" \
      -f "${1}" \
      --data-value serving.namespaces.system="${SYSTEM_NAMESPACE}"
}

function run_ytt() {
  go_run github.com/vmware-tanzu/carvel-ytt/cmd/ytt@v0.44.1 "$@"
}


function run_kapp() {
  go_run github.com/vmware-tanzu/carvel-kapp/cmd/kapp@v0.54.1 "$@"
}
