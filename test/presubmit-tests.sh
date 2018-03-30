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

# Useful environment variables
readonly ELAFROS_ROOT=$(dirname ${BASH_SOURCE})/..
[[ $USER == "prow" ]] && IS_PROW=1 || IS_PROW=0
readonly IS_PROW

# Save *_OVERRIDE variables in case a cleanup is required.
readonly OG_DOCKER_REPO="${DOCKER_REPO_OVERRIDE}"
readonly OG_K8S_CLUSTER="${K8S_CLUSTER_OVERRIDE}"
readonly OG_K8S_USER="${K8S_USER_OVERRIDE}"

function restore_env() {
  export DOCKER_REPO_OVERRIDE="${OG_DOCKER_REPO}"
  export K8S_CLUSTER_OVERRIDE="${OG_K8S_CLUSTER}"
  export K8S_USER_OVERRIDE="${OG_K8S_CLUSTER}"
}

function cleanup() {
  header "Cleanup (teardown)"
  restore_env
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
if (( ! IS_PROW )); then
  trap cleanup EXIT
  header "Cleanup (setup)"
  # --expunge is a workaround for https://github.com/elafros/elafros/issues/366
  bazel clean --expunge
fi

# Tests to be performed.

# Step 1: Build relevant packages to ensure nothing is broken.
header "Building phase"
bazel build //cmd/... //config/... //sample/... //pkg/... //test/...
bazel build :everything

# Step 2: Run unit tests.
header "Testing phase"
bazel test //cmd/... //pkg/...
# Run go tests as well to workaround https://github.com/elafros/elafros/issues/525
go test ./cmd/... ./pkg/...

# Step 3: Run end-to-end tests.
if (( ! IS_PROW )); then
  # Restore environment variables, as they are needed when running locally.
  restore_env
fi
./test/e2e-tests.sh