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

# Script entry point.

# Skip installing istio as an add-on
initialize $@ --skip-istio-addon

# Run the tests
header "Running tests"

failed=0

# Patch tests to run serially in the mesh scenario
(( ISTIO_MESH )) && find . -iname '*_test.go' | xargs sed -i -e '/^.*\.Parallel()/d'

# Run conformance and e2e tests.
if (( INSTALL_V1 )); then
  # When v1 is installed, run all our tests
  go_test_e2e -timeout=30m \
    ./test/conformance/... \
    ./test/e2e \
    "--resolvabledomain=$(use_resolvable_domain)" || failed=1
else
  go_test_e2e -timeout=30m \
    ./test/conformance/api/v1alpha1 \
    ./test/conformance/runtime \
    ./test/e2e \
    "--resolvabledomain=$(use_resolvable_domain)" || failed=1
fi

# Dump cluster state after e2e tests to prevent logs being truncated.
(( failed )) && dump_cluster_state

# Run scale tests.
go_test_e2e -timeout=10m ./test/scale || failed=1

# Require that both set of tests succeeded.
(( failed )) && fail_test

success
