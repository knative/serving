#!/bin/bash

# Copyright 2018 Google, Inc. All rights reserved.
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

# This script runs the presubmit tests, in the right order.
# It is started by prow for each PR.
# For convenience, it can also be executed manually.

set -o errexit
set -o pipefail

ELAFROS_ROOT=$(dirname ${BASH_SOURCE})/..
OG_DOCKER_REPO="${DOCKER_REPO_OVERRIDE}"
OG_K8S_CLUSTER="${K8S_CLUSTER_OVERRIDE}"
OG_K8S_USER="${K8S_USER_OVERRIDE}"

set -o nounset

function cleanup() {
  header "Cleanup (teardown)"
  export DOCKER_REPO_OVERRIDE="${OG_DOCKER_REPO}"
  export K8S_CLUSTER_OVERRIDE="${OG_K8S_CLUSTER}"
  export K8S_USER_OVERRIDE="${OG_K8S_CLUSTER}"
  # --expunge is a workaround for https://github.com/elafros/elafros/issues/366
  bazel clean --expunge || true
}

function header() {
  echo "================================================="
  echo $1
  echo "================================================="
}

cd ${ELAFROS_ROOT}

# Set the required env vars to dummy values to satisfy bazel.
export DOCKER_REPO_OVERRIDE=REPO_NOT_SET
export K8S_CLUSTER_OVERRIDE=CLUSTER_NOT_SET
export K8S_USER_OVERRIDE=USER_NOT_SET

# For local runs, cleanup before and after the tests.
if [[ ! $USER == "prow" ]]; then
  trap cleanup EXIT
  header "Cleanup (setup)"
  # --expunge is a workaround for https://github.com/elafros/elafros/issues/366
  bazel clean --expunge
fi

# Tests to be performed.

# Step 1: Build relevant packages to ensure nothing is broken.
header "Building phase"
bazel build //cmd/... //config/... //sample/... //pkg/... //test/...

# Step 2: Run tests.
header "Testing phase"
bazel test //cmd/... //pkg/...
