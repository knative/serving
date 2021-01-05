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

function install_istio() {
  if [[ -z "${ISTIO_VERSION:-}" ]]; then
    readonly ISTIO_VERSION="stable"
  fi

  if [[ -z "${NET_ISTIO_COMMIT:-}" ]]; then
    NET_ISTIO_COMMIT=$(head -n 1 ${1} | grep "# Generated when HEAD was" | sed 's/^.* //')
    echo "Got NET_ISTIO_COMMIT from ${1}: ${NET_ISTIO_COMMIT}"
  fi

  # TODO: remove this when all the net-istio.yaml in use contain a commit ID
  if [[ -z "${NET_ISTIO_COMMIT:-}" ]]; then
    NET_ISTIO_COMMIT="8102cd3d32f05be1c58260a9717d532a4a6d2f60"
    echo "Hard coded NET_ISTIO_COMMIT: ${NET_ISTIO_COMMIT}"
  fi

  # And checkout the setup script based on that commit.
  local NET_ISTIO_DIR=$(mktemp -d)
  (
    cd $NET_ISTIO_DIR \
      && git init \
      && git remote add origin https://github.com/knative-sandbox/net-istio.git \
      && git fetch --depth 1 origin $NET_ISTIO_COMMIT \
      && git checkout FETCH_HEAD
  )

  ISTIO_PROFILE="istio"
  if [[ -n "${KIND:-}" ]]; then
    ISTIO_PROFILE+="-kind"
  else
    ISTIO_PROFILE+="-ci"
  fi
  if [[ $MESH -eq 0 ]]; then
    ISTIO_PROFILE+="-no"
  fi
  ISTIO_PROFILE+="-mesh"
  ISTIO_PROFILE+=".yaml"

  if [[ -n "${CLUSTER_DOMAIN:-}" ]]; then
    sed -ie "s/cluster\.local/${CLUSTER_DOMAIN}/g" ${NET_ISTIO_DIR}/third_party/istio-${ISTIO_VERSION}/${ISTIO_PROFILE}
  fi

  echo ">> Installing Istio"
  echo "Istio version: ${ISTIO_VERSION}"
  echo "Istio profile: ${ISTIO_PROFILE}"
  ${NET_ISTIO_DIR}/third_party/istio-${ISTIO_VERSION}/install-istio.sh ${ISTIO_PROFILE}

  if [[ -n "${1:-}" ]]; then
    echo ">> Installing net-istio"
    echo "net-istio original YAML: ${1}"
    # Create temp copy in which we replace knative-serving by the test's system namespace.
    local YAML_NAME=$(mktemp -p $TMP_DIR --suffix=.$(basename "$1"))
    sed "s/namespace: \"*${KNATIVE_DEFAULT_NAMESPACE}\"*/namespace: ${SYSTEM_NAMESPACE}/g" ${1} > ${YAML_NAME}
    echo "net-istio patched YAML: $YAML_NAME"
    ko apply -f "${YAML_NAME}" --selector=networking.knative.dev/ingress-provider=istio || return 1

    CONFIGURE_ISTIO=${NET_ISTIO_DIR}/third_party/istio-${ISTIO_VERSION}/extras/configure-istio.sh
    if [[ -f "$CONFIGURE_ISTIO" ]]; then
      $CONFIGURE_ISTIO
    else
      echo "configure-istio.sh not found; skipping."
    fi

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
  local YAML_NAME=$(mktemp -p $TMP_DIR --suffix=.$(basename "$1"))
  sed "s/${KNATIVE_DEFAULT_NAMESPACE}/${SYSTEM_NAMESPACE}/g" ${INSTALL_KOURIER_YAML} > ${YAML_NAME}
  echo "Kourier YAML: ${YAML_NAME}"
  echo ">> Bringing up Kourier"

  kubectl apply -f ${YAML_NAME} || return 1
  UNINSTALL_LIST+=( "${YAML_NAME}" )

  echo ">> Patching Kourier"
  # Scale replicas of the Kourier gateways to handle large qps
  kubectl scale -n kourier-system deployment 3scale-kourier-gateway --replicas=6
}

function install_kong() {
  local INSTALL_KONG_YAML="./third_party/kong-latest/kong.yaml"
  echo "Kong YAML: ${INSTALL_KONG_YAML}"
  echo ">> Bringing up Kong"

  kubectl apply -f ${INSTALL_KONG_YAML} || return 1
  UNINSTALL_LIST+=( "${INSTALL_KONG_YAML}" )

  echo ">> Patching Kong"
  # Scale replicas of the Kong gateways to handle large qps
  kubectl scale -n kong deployment ingress-kong --replicas=6
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
  sed 's/--log-level info/--log-level debug/g' "${INSTALL_CONTOUR_YAML}" | kubectl apply -f - || return 1

  UNINSTALL_LIST+=( "${INSTALL_CONTOUR_YAML}" )
  HA_COMPONENTS+=( "contour-ingress-controller" )

  local NET_CONTOUR_YAML_NAME=${TMP_DIR}/${INSTALL_NET_CONTOUR_YAML##*/}
  sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${SYSTEM_NAMESPACE}/g" ${INSTALL_NET_CONTOUR_YAML} > ${NET_CONTOUR_YAML_NAME}
  echo ">> Bringing up net-contour"
  kubectl apply -f ${NET_CONTOUR_YAML_NAME} || return 1

  # Disable verbosity until https://github.com/golang/go/issues/40771 is fixed.
  export GO_TEST_VERBOSITY=standard-quiet

  UNINSTALL_LIST+=( "${NET_CONTOUR_YAML_NAME}" )
}

function wait_until_ingress_running() {
  if [[ -n "${ISTIO_VERSION:-}" ]]; then
    wait_until_pods_running istio-system || return 1
    wait_until_service_has_external_http_address istio-system istio-ingressgateway || return 1
  fi
  if [[ -n "${GLOO_VERSION:-}" ]]; then
    # we must set these override values to allow the test spoofing client to work with Gloo
    # see https://github.com/knative/pkg/blob/release-0.7/test/ingress/ingress.go#L37
    export GATEWAY_OVERRIDE=knative-external-proxy
    export GATEWAY_NAMESPACE_OVERRIDE=gloo-system
    wait_until_pods_running gloo-system || return 1
    wait_until_service_has_external_ip gloo-system knative-external-proxy
  fi
  if [[ -n "${KOURIER_VERSION:-}" ]]; then
    # we must set these override values to allow the test spoofing client to work with Kourier
    # see https://github.com/knative/pkg/blob/release-0.7/test/ingress/ingress.go#L37
    export GATEWAY_OVERRIDE=kourier
    export GATEWAY_NAMESPACE_OVERRIDE=kourier-system
    wait_until_pods_running kourier-system || return 1
    wait_until_service_has_external_http_address kourier-system kourier
  fi
  if [[ -n "${AMBASSADOR_VERSION:-}" ]]; then
    # we must set these override values to allow the test spoofing client to work with Ambassador
    # see https://github.com/knative/pkg/blob/release-0.7/test/ingress/ingress.go#L37
    export GATEWAY_OVERRIDE=ambassador
    export GATEWAY_NAMESPACE_OVERRIDE=ambassador
    wait_until_pods_running ambassador || return 1
    wait_until_service_has_external_http_address ambassador ambassador
  fi
  if [[ -n "${CONTOUR_VERSION:-}" ]]; then
    # we must set these override values to allow the test spoofing client to work with Contour
    # see https://github.com/knative/pkg/blob/release-0.7/test/ingress/ingress.go#L37
    export GATEWAY_OVERRIDE=envoy
    export GATEWAY_NAMESPACE_OVERRIDE=contour-external
    wait_until_pods_running contour-external || return 1
    wait_until_pods_running contour-internal || return 1
    wait_until_service_has_external_ip "${GATEWAY_NAMESPACE_OVERRIDE}" "${GATEWAY_OVERRIDE}"
  fi
  if [[ -n "${KONG_VERSION:-}" ]]; then
    # we must set these override values to allow the test spoofing client to work with Kong
    # see https://github.com/knative/pkg/blob/release-0.7/test/ingress/ingress.go#L37
    export GATEWAY_OVERRIDE=kong-proxy
    export GATEWAY_NAMESPACE_OVERRIDE=kong
    wait_until_pods_running kong || return 1
    wait_until_service_has_external_http_address kong kong-proxy
  fi
}
