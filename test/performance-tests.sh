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
  delete_everything
}

# Fail fast during setup.
set -o errexit
set -o pipefail

header "Setting up environment"

initialize $@
create_everything
publish_test_images

wait_until_cluster_up

create_namespace

# Handle test failures ourselves, so we can dump useful info.
set +o errexit
set +o pipefail

# Need to export concurrency var as it is required by the parser.
export concurrency=5
report_go_test -v -tags=performance -count=1 -timeout=5m ./test/performance || fail_test

success
