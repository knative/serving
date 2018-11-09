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
  # Delete the service now that the test is done
  kubectl delete --ignore-not-found=true -f ${TEST_APP_YAML}
  uninstall_knative_serving
}

initialize $@

header "Setting up environment"

# Handle test failures ourselves, so we can dump useful info.
set +o errexit
set +o pipefail

install_knative_serving || fail_test "Knative Serving installation failed"
create_prometheus || fail_test "Prometheus creation failed"
publish_test_images || fail_test "one or more test images weren't published"

create_namespace || fail_test "cannot create test namespace"

go_test_e2e -tags=performance -timeout=5m ./test/performance || fail_test

success