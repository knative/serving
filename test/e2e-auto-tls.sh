#!/bin/bash

function setup_auto_tls_env() {
  # DNS zone for the testing domain.
  export DNS_ZONE="knative-e2e"
  # Google Cloud project that hosts the DNS server for the testing domain `kn-e2e.dev`
  export CLOUD_DNS_PROJECT="knative-e2e-dns"
  # The service account credential file used to access the DNS server.
  export CLOUD_DNS_SERVICE_ACCOUNT_KEY_FILE="/etc/test-account/service-account.json"
  # The name of the Knative Service deployed in Auto TLS E2E test.
  export TLS_SERVICE_NAME="http01"

  export CUSTOM_DOMAIN_SUFFIX="$(($RANDOM % 1000)).${E2E_PROJECT_ID}.kn-e2e.dev"
  # The full host name of the Knative Service. This is used to configure the DNS record.
  export FULL_HOST_NAME="http01.serving-tests.${CUSTOM_DOMAIN_SUFFIX}"

  local INGRESS_NAMESPACE=${GATEWAY_NAMESPACE}
  if [[-z "${GATEWAY_NAMESPACE}"]]; then
    INGRESS_NAMESPACE="istio-system"
  fi
  local INGRESS_SERVICE=${GATEWAY_OVERRIDE}
  if [[-z "${GATEWAY_OVERRIDE}"]]; then
    INGRESS_NAMESPACE="istio-ingressgateway"
  fi 
  local IP=$(kubectl get svc -n ${INGRESS_NAMESPACE} ${INGRESS_SERVICE} -o jsonpath="{.status.loadBalancer.ingress[0].ip}")
  export INGRESS_IP=${IP}
}

function setup_custom_domain() {
  kubectl patch cm config-domain -n knative-serving -p '{"data":{"${CUSTOM_DOMAIN_SUFFIX}":""}}'
}

function setup_http01_auto_tls() {
  setup_auto_tls_env
  
  # Disable namespace certificate.
  unset NamespaceWithCert
  go run ./test/e2e/autotls/config/disablenscert

  # Clean up leftover certificates if there are
  kubectl delete kcert --all -n serving-tests

  kubectl apply -f test/config/autotls/certmanager/http01/
  setup_dns_record
}

function setup_dns_record() {
  go run ./test/e2e/autotls/config/dnssetup/
}