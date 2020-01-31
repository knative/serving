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

# This script runs the end-to-end tests against Knative Serving built from source.
# It is started by prow for each PR. For convenience, it can also be executed manually.

# If you already have a Knative cluster setup and kubectl pointing
# to it, call this script with the --run-tests arguments and it will use
# the cluster and run the tests.

# Calling this script without arguments will create a new cluster in
# project $PROJECT_ID, start knative in it, run the tests and delete the
# cluster.

source $(dirname $0)/e2e-common.sh

# Helper functions.

function knative_setup() {
  install_knative_serving
}

function setup_http01_env() {
  export DNS_ZONE="knative-e2e"
  export CLOUD_DNS_PROJECT="knative-e2e-dns"
  export SET_UP_DNS="true"
  export CLOUD_DNS_SERVICE_ACCOUNT_KEY_FILE="/etc/test-account/service-account.json"
  export AUTO_TLS_DOMAIN="${E2E_PROJECT_ID}.kn-e2e.dev"
}

# Script entry point.

# Skip installing istio as an add-on
initialize $@ --skip-istio-addon

# Run the tests
header "Running tests"

failed=0

# Run tests serially in the mesh scenario
parallelism=""
(( MESH )) && parallelism="-parallel 1"

# Run conformance and e2e tests.
go_test_e2e -timeout=30m \
  $(go list ./test/conformance/... | grep -v certificate) \
  ./test/e2e \
  ${parallelism} \
  "--resolvabledomain=$(use_resolvable_domain)" "$(use_https)" "$(ingress_class)" || failed=1

# Certificate conformance tests must be run separately
kubectl apply -f ./test/config/autotls/certmanager/selfsigned/
add_trap "kubectl delete -f ./test/config/autotls/certmanager/selfsigned/ --ignore-not-found" SIGKILL SIGTERM SIGQUIT
go_test_e2e -timeout=10m \
  ./test/conformance/certificate/nonhttp01 "$(certificate_class)" || failed=1
kubectl delete -f ./test/config/autotls/certmanager/selfsigned/

kubectl apply -f ./test/config/autotls/certmanager/http01/
add_trap "kubectl delete -f ./test/config/autotls/certmanager/http01/ --ignore-not-found" SIGKILL SIGTERM SIGQUIT
go_test_e2e -timeout=10m \
  ./test/conformance/certificate/http01 "$(certificate_class)" || failed=1
kubectl delete -f ./test/config/autotls/certmanager/http01/

# Run scale tests.
go_test_e2e -timeout=10m \
  ${parallelism} \
  ./test/scale || failed=1

# Auto TLS E2E tests mutate the cluster and must be ran separately
kubectl apply -f ./test/config/autotls/certmanager/selfsigned/
add_trap "kubectl delete -f ./test/config/autotls/certmanager/selfsigned/ --ignore-not-found" SIGKILL SIGTERM SIGQUIT
go_test_e2e -timeout=10m \
  ./test/e2e/autotls/nonhttp01 || failed=1
kubectl delete -f ./test/config/autotls/certmanager/selfsigned/

kubectl apply -f ./test/config/autotls/certmanager/http01/
add_trap "kubectl delete -f ./test/config/autotls/certmanager/http01/ --ignore-not-found" SIGKILL SIGTERM SIGQUIT
setup_http01_env
go_test_e2e -timeout=10m \
  ./test/e2e/autotls/http01 || failed=1

# Istio E2E tests mutate the cluster and must be ran separately
if [[ -n "${ISTIO_VERSION}" ]]; then
  go_test_e2e -timeout=10m \
    ./test/e2e/istio \
    "--resolvabledomain=$(use_resolvable_domain)" "$(use_https)" || failed=1
fi

# Dump cluster state in case of failure
(( failed )) && dump_cluster_state
(( failed )) && fail_test

success
