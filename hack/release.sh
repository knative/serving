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

source "$(dirname $(readlink -f ${BASH_SOURCE}))/../test/library.sh"

# Set default GCS/GCR
: ${ELAFROS_RELEASE_GCS:="elafros-releases/latest"}
: ${ELAFROS_RELEASE_GCR:="gcr.io/elafros-releases"}
readonly ELAFROS_RELEASE_GCS
readonly ELAFROS_RELEASE_GCR

function cleanup() {
  restore_override_vars
  bazel clean --expunge || true
}

function banner() {
  echo "@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@"
  echo "@@@@ $1 @@@@"
  echo "@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@"
}

cd ${ELAFROS_ROOT_DIR}
trap cleanup EXIT

if [[ "$1" != "--skip-tests" ]]; then
  banner "RUNNING RELEASE VALIDATION TESTS"
  # Run tests.
  ./test/presubmit-tests.sh
fi

banner "    BUILDING THE RELEASE   "

# Set the repository
export DOCKER_REPO_OVERRIDE=${ELAFROS_RELEASE_GCR}
# Build should not try to deploy anything, use a bogus value for cluster.
export K8S_CLUSTER_OVERRIDE=CLUSTER_NOT_SET
export K8S_USER_OVERRIDE=USER_NOT_SET

# If this is a prow job, authenticate against GCR.
if (( IS_PROW )); then
  gcr_auth
fi

echo "- Destination GCR: ${ELAFROS_RELEASE_GCR}"
echo "- Destination GCS: ${ELAFROS_RELEASE_GCS}"

echo "Cleaning up"
bazel clean --expunge
echo "Copying Build release"
cp ${ELAFROS_ROOT_DIR}/third_party/config/build/release.yaml release.yaml
echo "---" >> release.yaml
echo "Building Elafros"
bazel run config:everything >> release.yaml
echo "---" >> release.yaml
echo "Building Monitoring & Logging"
bazel run config/monitoring:everything >> release.yaml

echo "Publishing release.yaml"
gsutil cp release.yaml gs://${ELAFROS_RELEASE_GCS}/release.yaml

echo "New release published successfully"

# TODO(mattmoor): Create other aliases?
