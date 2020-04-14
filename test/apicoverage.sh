#!/usr/bin/env bash

# Copyright 2019 The Knative Authors
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

# If you already have the *_OVERRIDE environment variables set, call
# this script with the --run-tests arguments and it will start knative in
# the cluster and run the tests.

# Calling this script without arguments will create a new cluster in
# project $PROJECT_ID, start knative in it, run the tests and delete the
# cluster.

source $(dirname $0)/e2e-common.sh

readonly SERVING_TEST_DIR=$(dirname $0)
readonly APICOVERAGE_IMAGE="${SERVING_TEST_DIR}/apicoverage/image"
readonly APICOVERAGE_TOOL="${SERVING_TEST_DIR}/apicoverage/tools"

function knative_setup() {
  install_knative_serving
}

# Wrapper to fail_test to write apicoverage junit result file.
# Parameters: $1 - Error message to pass onto fail_test
function fail_apicoverage_run() {
  go run "${APICOVERAGE_TOOL}/main.go" --build_failed=true
  fail_test $1
}

# Script entry point.
initialize $@ --skip-istio-addon

header "Setting up API Coverage Webhook"
kubectl apply -f "${TMP_DIR}/test/apicoverage/image/service-account.yaml" || fail_apicoverage_run "Failed setting up service account for apicoverage-webhook"
ko apply -f "${TMP_DIR}/test/apicoverage/image/apicoverage-webhook.yaml" || fail_apicoverage_run "Failed setting up apicoverage-webhook"

header "Running tests"
# Run conformance tests and e2e tests
go_test_e2e -timeout=30m ./test/conformance/api/v1alpha1 ./test/conformance/api/v1beta1 ./test/conformance/runtime ./test/e2e \
  || fail_apicoverage_run "Failed in executing Tests"

header "Retrieving API Coverage values"
go run "${APICOVERAGE_TOOL}/main.go" || fail_test "Failed retrieving API coverage values"

success
