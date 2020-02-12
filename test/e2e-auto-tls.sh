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

# This script runs the end-to-end tests against Knative Serving built from source.
# It is started by prow for each PR. For convenience, it can also be executed manually.

# If you already have a Knative cluster setup and kubectl pointing
# to it, call this script with the --run-tests arguments and it will use
# the cluster and run the tests.

# Calling this script without arguments will create a new cluster in
# project $PROJECT_ID, start knative in it, run the tests and delete the
# cluster.

function setup_auto_tls_env_variables() {
  # DNS zone for the testing domain.
  export DNS_ZONE="knative-e2e"
  # Google Cloud project that hosts the DNS server for the testing domain `kn-e2e.dev`
  export CLOUD_DNS_PROJECT="knative-e2e-dns"
  # The service account credential file used to access the DNS server.
  export CLOUD_DNS_SERVICE_ACCOUNT_KEY_FILE="/etc/test-account/service-account.json"

  export CUSTOM_DOMAIN_SUFFIX="$(($RANDOM % 10000)).${E2E_PROJECT_ID}.zhiminx.info"

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
  kubectl patch cm config-domain -n knative-serving -p '{"data":{"${CUSTOM_DOMAIN_SUFFIX}":""}}'
}

function cleanup_custom_domain() {
  kubectl apply -f ./config/config-domain.yaml
}

function turn_on_auto_tls() {
  kubectl apply -f ./test/config/autotls/config-network.yaml
}

function turn_off_auto_tls() {
  kubectl apply -f ./config/config-network.yaml
}

function setup_auto_tls_common() {
  setup_auto_tls_env_variables

  setup_custom_domain

  # Disable namespace certificate.
  unset NamespaceWithCert
  go run ./test/e2e/autotls/config/disablenscert

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

  # Disable namespace certificate.
  unset NamespaceWithCert
  go run ./test/e2e/autotls/config/disablenscert

  kubectl delete kcert --all -n serving-tests

  kubectl apply -f test/config/autotls/certmanager/http01/
  setup_dns_record
}

function setup_selfsigned_per_ksvc_auto_tls() {
  # The name of the test.
  export AUTO_TLS_TEST_NAME="SelfSignedPerKsvc"
  # The name of the Knative Service deployed in Auto TLS E2E test.
  export TLS_SERVICE_NAME="self-per-ksvc"

  # Disable namespace certificate.
  unset NamespaceWithCert
  go run ./test/e2e/autotls/config/disablenscert

  kubectl delete kcert --all -n serving-tests
  kubectl apply -f test/config/autotls/certmanager/selfsigned/
}

function setup_selfsigned_per_namespace_auto_tls() {
  # The name of the test.
  export AUTO_TLS_TEST_NAME="SelfSignedPerNamespace"
  # The name of the Knative Service deployed in Auto TLS E2E test.
  export TLS_SERVICE_NAME="self-per-namespace"

  kubectl delete kcert --all -n serving-tests

  # Enable namespace certificate.
  export NamespaceWithCert="serving-tests"
  go run ./test/e2e/autotls/config/disablenscert

  kubectl apply -f test/config/autotls/certmanager/selfsigned/
}

function setup_dns_record() {
  go run ./test/e2e/autotls/config/dnssetup/
}

function delete_dns_record() {
  go run ./test/e2e/autotls/config/dnscleanup/
}