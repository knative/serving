#!/usr/bin/env bash

# Copyright 2020 The Knative Authors
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

function is_ingress_class() {
  [[ "${INGRESS_CLASS}" == *"${1}"* ]]
}

function stage_istio_head() {
  header "Staging Istio YAML (HEAD)"
  local istio_head_dir="${E2E_YAML_DIR}/istio/HEAD/install"
  mkdir -p "${istio_head_dir}"
  download_net_istio_yamls "${REPO_ROOT_DIR}/third_party/istio-latest/net-istio.yaml" "${istio_head_dir}"
}

function stage_istio_latest() {
  header "Staging Istio YAML (${LATEST_NET_ISTIO_RELEASE_VERSION})"
  local istio_latest_dir="${E2E_YAML_DIR}/istio/latest-release/install"
  mkdir -p "${istio_latest_dir}"

  download_net_istio_yamls \
    "https://github.com/knative-sandbox/net-istio/releases/download/${LATEST_NET_ISTIO_RELEASE_VERSION}/net-istio.yaml" \
    "${istio_latest_dir}"
}

function download_net_istio_yamls() {
  local net_istio_yaml="$1"
  local target_dir="$2"

  if [[ "${net_istio_yaml}" == "http"* ]]; then
    wget "${net_istio_yaml}" -P "${target_dir}" \
      || fail_test "Unable to download istio file ${net_istio_yaml}"
  else
    cp "${net_istio_yaml}" "${target_dir}"
  fi

  # Point to our local copy
  net_istio_yaml="${target_dir}/$(basename "${net_istio_yaml}")"

  local sha=$(head -n 1 ${net_istio_yaml} | grep "# Generated when HEAD was" | sed 's/^.* //')
  if [[ -z "${sha:-}" ]]; then
    sha="191bc5fe5a4b35b64f70577c3e44e44fb699cc5f"
    echo "Hard coded NET_ISTIO_COMMIT: ${sha}"
  else
    echo "Got NET_ISTIO_COMMIT from ${1}: ${sha}"
  fi

  local istio_yaml="$(net_istio_file_url "$sha" istio.yaml)"
  local istio_config_yaml="$(net_istio_file_url "$sha" config-istio.yaml)"

  wget -P "${target_dir}" "${istio_yaml}" \
    || fail_test "Unable to get istio install file ${istio_yaml}"

  # Some istio profiles don't have a config-istio so do a HEAD request to check
  # before downloading
  if wget -S --spider "${istio_config_yaml}" &> /dev/null; then
    wget -P "${target_dir}" "${istio_config_yaml}" \
      || fail_test "Unable to get istio install file ${istio_config_yaml}"
  else
    echo "istio profile does not have a config-istio.yaml upstream"
  fi
}

function net_istio_file_url() {
  local sha="$1"
  local file="$2"

  local profile="istio"
  if (( KIND )); then
    profile+="-kind"
  else
    profile+="-ci"
  fi
  if [[ $MESH -eq 0 ]]; then
    profile+="-no"
  fi

  profile+="-mesh"

  echo "https://raw.githubusercontent.com/knative-sandbox/net-istio/${sha}/third_party/istio-${ISTIO_VERSION}/${profile}/${file}"
}

function setup_ingress_env_vars() {
  if is_ingress_class istio; then
    export GATEWAY_OVERRIDE=istio-ingressgateway
    export GATEWAY_NAMESPACE_OVERRIDE=istio-system
  fi
  if is_ingress_class kourier; then
    export GATEWAY_OVERRIDE=kourier
    export GATEWAY_NAMESPACE_OVERRIDE=kourier-system
  fi
  if is_ingress_class ambassador; then
    export GATEWAY_OVERRIDE=ambassador
    export GATEWAY_NAMESPACE_OVERRIDE=ambassador
  fi
  if is_ingress_class contour; then
    export GATEWAY_OVERRIDE=envoy
    export GATEWAY_NAMESPACE_OVERRIDE=contour-external
  fi
  if is_ingress_class kong; then
    export GATEWAY_OVERRIDE=kong-proxy
    export GATEWAY_NAMESPACE_OVERRIDE=kong
  fi
}

