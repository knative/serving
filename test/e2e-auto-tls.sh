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

function setup_auto_tls_env_variables() {
  # DNS zone for the testing domain.
  export DNS_ZONE="knative-e2e"
  # Google Cloud project that hosts the DNS server for the testing domain `kn-e2e.dev`
  export CLOUD_DNS_PROJECT="knative-e2e-dns"
  # The service account credential file used to access the DNS server.
  export CLOUD_DNS_SERVICE_ACCOUNT_KEY_FILE="/etc/test-account/service-account.json"

  export CUSTOM_DOMAIN_SUFFIX="$(($RANDOM % 10000)).${E2E_PROJECT_ID}.kn-e2e.dev"

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
  namespace: knative-serving
  labels:
    serving.knative.dev/release: devel
data:
  ${CUSTOM_DOMAIN_SUFFIX}: ""
EOF
}

function cleanup_custom_domain() {
  kubectl apply -f ./config/config-domain.yaml
}

function turn_on_auto_tls() {
  kubectl patch configmap config-network -n knative-serving -p '{"data":{"autoTLS":"Enabled"}}'
}

function turn_off_auto_tls() {
  kubectl patch configmap config-network -n knative-serving -p '{"data":{"autoTLS":"Disabled"}}'
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
  export FULL_HOST_NAME="${TLS_SERVICE_NAME}.serving-tests.${CUSTOM_DOMAIN_SUFFIX}"

  kubectl delete kcert --all -n serving-tests

  kubectl apply -f test/config/autotls/certmanager/http01/
  setup_dns_record
}

function setup_selfsigned_per_ksvc_auto_tls() {
  # The name of the test.
  export AUTO_TLS_TEST_NAME="SelfSignedPerKsvc"
  # The name of the Knative Service deployed in Auto TLS E2E test.
  export TLS_SERVICE_NAME="self-per-ksvc"

  kubectl delete kcert --all -n serving-tests
  kubectl apply -f test/config/autotls/certmanager/selfsigned/
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

  kubectl apply -f test/config/autotls/certmanager/selfsigned/

  # SERVING_NSCERT_YAML is set in build_knative_from_source function
  # when building knative.
  echo "Intall namespace cert controller: ${SERVING_NSCERT_YAML}"
  if [[ -z "${SERVING_NSCERT_YAML}" ]]; then
    echo "Error: variable SERVING_NSCERT_YAML is not set."
    exit 1
  fi
  kubectl apply -f ${SERVING_NSCERT_YAML}
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
