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

# This script runs the performance tests; It is run by prow daily.
# For convenience, it can also be executed manually.

source $(dirname $0)/cluster.sh

# Deletes everything created on the cluster including all knative and istio components.
function teardown() {
  uninstall_knative_serving
}

initialize $@

header "Setting up environment"

# Handle test failures ourselves, so we can dump useful info.
set +o errexit
set +o pipefail

# Build Knative, but don't install the default "no monitoring" version
build_knative_from_source
install_knative_serving "${ISTIO_CRD_YAML}" "${ISTIO_YAML}" "${SERVING_YAML}" \
    || fail_test "Knative Serving installation failed"
publish_test_images || fail_test "one or more test images weren't published"

# Run the tests
# We use a plain `go test` because `go_test_e2e()` calls bazel to generate
# the test summary, thus overwriting our generated performance summary
go test -v -count=1 -tags=performance -timeout=5m ./test/performance || fail_test

success
