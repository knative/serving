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

source $(dirname "$0")/e2e-common.sh

function setup_auto_tls_env_variables() {
  # DNS zone for the testing domain.
  export AUTO_TLS_TEST_DNS_ZONE="knative-e2e"
  # Google Cloud project that hosts the DNS server for the testing domain `kn-e2e.dev`
  export AUTO_TLS_TEST_CLOUD_DNS_PROJECT="knative-e2e-dns"
  # The service account credential file used to access the DNS server.
  export AUTO_TLS_TEST_CLOUD_DNS_SERVICE_ACCOUNT_KEY_FILE="${GOOGLE_APPLICATION_CREDENTIALS}"

  export AUTO_TLS_TEST_DOMAIN_NAME="kn-e2e.dev"

  export CUSTOM_DOMAIN_SUFFIX="$(($RANDOM % 10000)).${E2E_PROJECT_ID}.${AUTO_TLS_TEST_DOMAIN_NAME}"

  export TLS_TEST_NAMESPACE="tls"

  local INGRESS_NAMESPACE=${GATEWAY_NAMESPACE_OVERRIDE}
  if [[ -z "${GATEWAY_NAMESPACE_OVERRIDE}" ]]; then
    INGRESS_NAMESPACE="istio-system"
  fi
  local INGRESS_SERVICE=${GATEWAY_OVERRIDE}
  if [[ -z "${GATEWAY_OVERRIDE}" ]]; then
    INGRESS_SERVICE="istio-ingressgateway"
  fi
  local IP=$(kubectl get svc -n ${INGRESS_NAMESPACE} ${INGRESS_SERVICE} -o jsonpath="{.status.loadBalancer.ingress[0].ip}")
  export AUTO_TLS_TEST_INGRESS_IP=${IP}
}

function setup_custom_domain() {
  echo ">> Configuring custom domain for Auto TLS tests: ${CUSTOM_DOMAIN_SUFFIX}"
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-domain
  namespace: ${SYSTEM_NAMESPACE}
  labels:
    app.kubernetes.io/name: knative-serving
    app.kubernetes.io/version: devel
data:
  ${CUSTOM_DOMAIN_SUFFIX}: ""
EOF
}

function cleanup_custom_domain() {
  kubectl delete ConfigMap config-domain -n ${SYSTEM_NAMESPACE}
}

function setup_auto_tls_common() {
  setup_auto_tls_env_variables

  setup_custom_domain

  toggle_feature auto-tls Enabled config-network
  toggle_feature autocreate-cluster-domain-claims true config-network
}

function cleanup_auto_tls_common() {
  cleanup_custom_domain

  toggle_feature auto-tls Disabled config-network
  toggle_feature autocreate-cluster-domain-claims false config-network
  toggle_feature namespace-wildcard-cert-selector "" config-network
  kubectl delete kcert --all -n "${TLS_TEST_NAMESPACE}"
}

function setup_http01_auto_tls() {
    # The name of the test, lowercase to avoid hyphenation of the test name.
  export AUTO_TLS_TEST_NAME="http01"
  # Rely on the built-in naming (for logstream)
  unset TLS_SERVICE_NAME
  # The full host name of the Knative Service. This is used to configure the DNS record.
  export AUTO_TLS_TEST_FULL_HOST_NAME="*.${CUSTOM_DOMAIN_SUFFIX}"

  kubectl delete kcert --all -n "${TLS_TEST_NAMESPACE}"

  if [[ -z "${MESH}" ]]; then
    echo "Install cert-manager no-mesh ClusterIssuer"
    kubectl apply -f "${E2E_YAML_DIR}"/test/config/autotls/certmanager/http01/issuer.yaml
  else
    echo "Install cert-manager mesh ClusterIssuer"
    kubectl apply -f "${E2E_YAML_DIR}"/test/config/autotls/certmanager/http01/mesh-issuer.yaml
  fi
  kubectl apply -f "${E2E_YAML_DIR}"/test/config/autotls/certmanager/http01/config-certmanager.yaml
  setup_dns_record
}

function setup_selfsigned_per_ksvc_auto_tls() {
  # The name of the test.
  export AUTO_TLS_TEST_NAME="SelfSignedPerKsvc"
  # The name of the Knative Service deployed in Auto TLS E2E test.
  export TLS_SERVICE_NAME="self-per-ksvc"

  kubectl delete kcert --all -n "${TLS_TEST_NAMESPACE}"
  kubectl apply -f ${E2E_YAML_DIR}/test/config/autotls/certmanager/selfsigned/
}

function setup_selfsigned_per_namespace_auto_tls() {
  # The name of the test.
  export AUTO_TLS_TEST_NAME="SelfSignedPerNamespace"
  # The name of the Knative Service deployed in Auto TLS E2E test.
  export TLS_SERVICE_NAME="self-per-namespace"

  kubectl delete kcert --all -n "${TLS_TEST_NAMESPACE}"

  # Enable namespace certificate only for "${TLS_TEST_NAMESPACE}" namespaces
  selector="matchExpressions:
  - key: kubernetes.io/metadata.name
    operator: In
    values: [${TLS_TEST_NAMESPACE}]
  "
  toggle_feature namespace-wildcard-cert-selector "$selector" config-network

  kubectl apply -f ${E2E_YAML_DIR}/test/config/autotls/certmanager/selfsigned/

}

function cleanup_per_selfsigned_namespace_auto_tls() {
  # Disable namespace cert for all namespaces
  toggle_feature namespace-wildcard-cert-selector "" config-network

  kubectl delete -f ${E2E_YAML_DIR}/test/config/autotls/certmanager/selfsigned/ --ignore-not-found=true
}

function setup_dns_record() {
  go run ./test/e2e/autotls/config/dnssetup/
  if [ $? -eq 0 ]; then
    echo "Successfully set up DNS record"
  else
    echo "Error setting up DNS record"
    exit 1
  fi
}

function delete_dns_record() {
  go run ./test/e2e/autotls/config/dnscleanup/
  if [ $? -eq 0 ]; then
    echo "Successfully tore down DNS record"
  else
    echo "Error deleting up DNS record"
    exit 1
  fi
}

# Script entry point.
initialize "$@" --num-nodes=4 --enable-ha --cluster-version=1.26

# Run the tests
header "Running tests"

failed=0

# Currently only Istio, Contour and Kourier implement the alpha features.
alpha=""
if [[ -z "${INGRESS_CLASS}" \
  || "${INGRESS_CLASS}" == "istio.ingress.networking.knative.dev" \
  || "${INGRESS_CLASS}" == "contour.ingress.networking.knative.dev" \
  || "${INGRESS_CLASS}" == "kourier.ingress.networking.knative.dev" ]]; then
  alpha="--enable-alpha"
fi

AUTO_TLS_TEST_OPTIONS="${AUTO_TLS_TEST_OPTIONS:-${alpha} --enable-beta}"

# Auto TLS E2E tests mutate the cluster and must be ran separately
# because they need auto-tls and cert-manager specific configurations
subheader "Setup auto tls"
setup_auto_tls_common
add_trap "cleanup_auto_tls_common" EXIT SIGKILL SIGTERM SIGQUIT

subheader "Auto TLS test for per-ksvc certificate provision using self-signed CA"
setup_selfsigned_per_ksvc_auto_tls
go_test_e2e -timeout=10m ./test/e2e/autotls/ ${AUTO_TLS_TEST_OPTIONS} || failed=1
kubectl delete -f ${E2E_YAML_DIR}/test/config/autotls/certmanager/selfsigned/

subheader "Auto TLS test for per-namespace certificate provision using self-signed CA"
setup_selfsigned_per_namespace_auto_tls
add_trap "cleanup_per_selfsigned_namespace_auto_tls" SIGKILL SIGTERM SIGQUIT
go_test_e2e -timeout=10m ./test/e2e/autotls/ ${AUTO_TLS_TEST_OPTIONS} || failed=1
cleanup_per_selfsigned_namespace_auto_tls

if [[ ${RUN_HTTP01_AUTO_TLS_TESTS} -eq 1 ]]; then
  subheader "Auto TLS test for per-ksvc certificate provision using HTTP01 challenge"
  setup_http01_auto_tls
  add_trap "delete_dns_record" SIGKILL SIGTERM SIGQUIT
  go_test_e2e -timeout=10m ./test/e2e/autotls/ ${AUTO_TLS_TEST_OPTIONS} || failed=1
  kubectl delete -f ${E2E_YAML_DIR}/test/config/autotls/certmanager/http01/
  delete_dns_record
fi

(( failed )) && fail_test

subheader "Cleanup auto tls"
cleanup_auto_tls_common

# Remove the kail log file if the test flow passes.
# This is for preventing too many large log files to be uploaded to GCS in CI.
rm "${ARTIFACTS}/k8s.log-$(basename "${E2E_SCRIPT}").txt"
success
