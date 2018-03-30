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

set -o errexit
set -o pipefail

readonly ELAFROS_ROOT=$(dirname ${BASH_SOURCE})/..
readonly OG_DOCKER_REPO="${DOCKER_REPO_OVERRIDE}"
readonly OG_K8S_CLUSTER="${K8S_CLUSTER_OVERRIDE}"
readonly OG_K8S_USER="${K8S_USER_OVERRIDE}"

function header() {
  echo "*************************************************"
  echo "** $1"
  echo "*************************************************"
}

function cleanup() {
  export DOCKER_REPO_OVERRIDE="${OG_DOCKER_REPO}"
  export K8S_CLUSTER_OVERRIDE="${OG_K8S_CLUSTER}"
  export K8S_USER_OVERRIDE="${OG_K8S_CLUSTER}"
  bazel clean --expunge || true
}

cd ${ELAFROS_ROOT}
trap cleanup EXIT

header "TEST PHASE"

# Run tests.
./test/presubmit-tests.sh

header "BUILD PHASE"

# Set the repository to the official one:
export DOCKER_REPO_OVERRIDE=gcr.io/elafros-releases
# Build should not try to deploy anything, use a bogus value for cluster.
export K8S_CLUSTER_OVERRIDE=CLUSTER_NOT_SET
export K8S_USER_OVERRIDE=USER_NOT_SET

# If this is a prow job, authenticate against GCR.
if [[ $USER == "prow" ]]; then
  echo "Authenticating to GCR"
  key="$(cat /etc/service-account/service-account.json)"
  echo $key
  docker login -u _json_key -p "$key" https://gcr.io
  echo "Authenticated"
fi

echo "Cleaning up"
bazel clean --expunge
# TODO(mattmoor): Remove this once we depend on Build CRD releases
echo "Building build-crd"
bazel run @buildcrd//:everything > release.yaml
echo "---" >> release.yaml
echo "Building Elafros"
bazel run :elafros >> release.yaml

echo "Publishing release.yaml"
gsutil cp release.yaml gs://elafros-releases/latest/release.yaml

echo "New release published successfully"

# TODO(mattmoor): Create other aliases?
