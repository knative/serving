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

source $(dirname $0)/e2e-common.sh

function knative_setup() {
  install_knative_serving
}

function setup_auto_tls_env_variables() {
  # DNS zone for the testing domain.
  export DNS_ZONE="knative-e2e"
  # Google Cloud project that hosts the DNS server for the testing domain `kn-e2e.dev`
  export CLOUD_DNS_PROJECT="knative-e2e-dns"
  # The service account credential file used to access the DNS server.
  export CLOUD_DNS_SERVICE_ACCOUNT_KEY_FILE="${GOOGLE_APPLICATION_CREDENTIALS}"

  export DOMAIN_NAME="kn-e2e.dev"

  export CUSTOM_DOMAIN_SUFFIX="$(($RANDOM % 10000)).${E2E_PROJECT_ID}.${DOMAIN_NAME}"

  local INGRESS_NAMESPACE=${GATEWAY_NAMESPACE}
  if [[ -z "${GATEWAY_NAMESPACE}" ]]; then
    INGRESS_NAMESPACE="istio-system"
  fi
  local INGRESS_SERVICE=${GATEWAY_OVERRIDE}
  if [[ -z "${GATEWAY_OVERRIDE}" ]]; then
    INGRESS_SERVICE="istio-ingressgateway"
  fi
  local IP=$(kubectl get svc -n ${INGRESS_NAMESPACE} ${INGRESS_SERVICE} -o jsonpath="{.status.loadBalancer.ingress[0].ip}")
  export INGRESS_IP=${IP}
}

function setup_custom_domain() {
  echo ">> Configuring custom domain for Auto TLS tests: ${CUSTOM_DOMAIN_SUFFIX}"
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-domain
  namespace: ${E2E_SYSTEM_NAMESPACE}
  labels:
    serving.knative.dev/release: devel
data:
  ${CUSTOM_DOMAIN_SUFFIX}: ""
EOF
}

function cleanup_custom_domain() {
  kubectl delete ConfigMap config-domain -n ${E2E_SYSTEM_NAMESPACE}
}

function setup_auto_tls_common() {
  setup_auto_tls_env_variables

  setup_custom_domain

  turn_on_auto_tls
}

function cleanup_auto_tls_common() {
  cleanup_custom_domain

  turn_off_auto_tls
  kubectl delete kcert --all -n serving-tests
}

function setup_http01_auto_tls() {
    # The name of the test.
  export AUTO_TLS_TEST_NAME="HTTP01"
  # The name of the Knative Service deployed in Auto TLS E2E test.
  export TLS_SERVICE_NAME="http01"
  # The full host name of the Knative Service. This is used to configure the DNS record.
  export FULL_HOST_NAME="*.serving-tests.${CUSTOM_DOMAIN_SUFFIX}"

  kubectl delete kcert --all -n serving-tests

  if [[ -z "${MESH}" ]]; then
    echo "Install cert-manager no-mesh ClusterIssuer"
    kubectl apply -f ${TMP_DIR}/test/config/autotls/certmanager/http01/issuer.yaml
  else
    echo "Install cert-manager mesh ClusterIssuer"
    kubectl apply -f ${TMP_DIR}/test/config/autotls/certmanager/http01/mesh-issuer.yaml
  fi
  kubectl apply -f ${TMP_DIR}/test/config/autotls/certmanager/http01/config-certmanager.yaml
  setup_dns_record
}

function setup_selfsigned_per_ksvc_auto_tls() {
  # The name of the test.
  export AUTO_TLS_TEST_NAME="SelfSignedPerKsvc"
  # The name of the Knative Service deployed in Auto TLS E2E test.
  export TLS_SERVICE_NAME="self-per-ksvc"

  kubectl delete kcert --all -n serving-tests
  kubectl apply -f ${TMP_DIR}/test/config/autotls/certmanager/selfsigned/
}

function setup_selfsigned_per_namespace_auto_tls() {
  # The name of the test.
  export AUTO_TLS_TEST_NAME="SelfSignedPerNamespace"
  # The name of the Knative Service deployed in Auto TLS E2E test.
  export TLS_SERVICE_NAME="self-per-namespace"

  kubectl delete kcert --all -n serving-tests

  # Enable namespace certificate only for serving-tests namespaces
  export NAMESPACE_WITH_CERT="serving-tests"
  go run ./test/e2e/autotls/config/disablenscert

  kubectl apply -f ${TMP_DIR}/test/config/autotls/certmanager/selfsigned/

  # SERVING_NSCERT_YAML is set in build_knative_from_source function
  # when building knative.
  echo "Intall namespace cert controller: ${SERVING_NSCERT_YAML}"
  if [[ -z "${SERVING_NSCERT_YAML}" ]]; then
    echo "Error: variable SERVING_NSCERT_YAML is not set."
    exit 1
  fi
  local YAML_NAME=${TMP_DIR}/${SERVING_NSCERT_YAML##*/}
  sed "s/namespace: ${KNATIVE_DEFAULT_NAMESPACE}/namespace: ${E2E_SYSTEM_NAMESPACE}/g" ${SERVING_NSCERT_YAML} > ${YAML_NAME}
  kubectl apply -f ${YAML_NAME}
}

function cleanup_per_selfsigned_namespace_auto_tls() {
  # Disable namespace cert for all namespaces
  unset NAMESPACE_WITH_CERT
  go run ./test/e2e/autotls/config/disablenscert

  echo "Uninstall namespace cert controller"
  kubectl delete -f ${SERVING_NSCERT_YAML} --ignore-not-found=true

  kubectl delete kcert --all -n serving-tests
  kubectl delete -f ./test/config/autotls/certmanager/selfsigned/ --ignore-not-found=true
}

function setup_dns_record() {
  go run ./test/e2e/autotls/config/dnssetup/
}

function delete_dns_record() {
  go run ./test/e2e/autotls/config/dnscleanup/
}

# Script entry point.

# Skip installing istio as an add-on
initialize $@ --skip-istio-addon

# Run the tests
header "Running tests"

failed=0

# Auto TLS E2E tests mutate the cluster and must be ran separately
# because they need auto-tls and cert-manager specific configurations
subheader "Setup auto tls"
setup_auto_tls_common
add_trap "cleanup_auto_tls_common" EXIT SIGKILL SIGTERM SIGQUIT

subheader "Auto TLS test for per-ksvc certificate provision using self-signed CA"
setup_selfsigned_per_ksvc_auto_tls
go_test_e2e -timeout=10m \
  ./test/e2e/autotls/ --systemNamespace=${E2E_SYSTEM_NAMESPACE} || failed=1
kubectl delete -f ./test/config/autotls/certmanager/selfsigned/

subheader "Auto TLS test for per-namespace certificate provision using self-signed CA"
setup_selfsigned_per_namespace_auto_tls
add_trap "cleanup_per_selfsigned_namespace_auto_tls" SIGKILL SIGTERM SIGQUIT
go_test_e2e -timeout=10m \
  ./test/e2e/autotls/ --systemNamespace=${E2E_SYSTEM_NAMESPACE} || failed=1
cleanup_per_selfsigned_namespace_auto_tls

subheader "Auto TLS test for per-ksvc certificate provision using HTTP01 challenge"
setup_http01_auto_tls
add_trap "delete_dns_record" SIGKILL SIGTERM SIGQUIT
go_test_e2e -timeout=10m \
  ./test/e2e/autotls/ --systemNamespace=${E2E_SYSTEM_NAMESPACE} || failed=1
kubectl delete -f ./test/config/autotls/certmanager/http01/
delete_dns_record

subheader "Cleanup auto tls"
cleanup_auto_tls_common

# Dump cluster state in case of failure
(( failed )) && dump_cluster_state
(( failed )) && fail_test

success
